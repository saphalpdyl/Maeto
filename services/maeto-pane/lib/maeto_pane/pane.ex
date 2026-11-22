defmodule MaetoPane.Pane do
  @moduledoc "Latest desired intent and observed dataplane state for every node."

  use GenServer

  require Logger

  alias Gnat.Jetstream.API.KV

  @intents_bucket "maeto-intents"
  @state_bucket "maeto-node-state"
  @control_bucket "maeto-pane-control"
  @topic "pane"
  @retry_after 2_000
  @heartbeat_keep_ms 25 * 60 * 1_000

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def snapshot, do: GenServer.call(__MODULE__, :snapshot)

  def subscribe, do: Phoenix.PubSub.subscribe(MaetoPane.PubSub, @topic)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    send(self(), :watch)
    send(self(), :seed_heartbeats)

    {:ok, Map.put(empty(), :started_at, System.system_time(:millisecond))}
  end

  @impl true
  def handle_call(:snapshot, _from, state) do
    {:reply, Map.merge(empty(), state), state}
  end

  defp empty do
    %{
      intents: %{},
      states: %{},
      heartbeats: %{},
      started_at: System.system_time(:millisecond),
      control: nil,
      connected: false,
      watchers: []
    }
  end

  @impl true
  def handle_info(:watch, state) do
    case start_watchers() do
      {:ok, watchers} ->
        {:noreply, Map.merge(state, %{connected: true, watchers: watchers})}

      {:error, reason} ->
        Logger.debug("kv watch unavailable, retrying: #{describe(reason)}")
        Process.send_after(self(), :watch, @retry_after)

        {:noreply, Map.merge(state, %{connected: false, watchers: []})}
    end
  end

  def handle_info(:seed_heartbeats, state) do
    now = System.system_time(:millisecond)
    since = DateTime.add(DateTime.utc_now(), -@heartbeat_keep_ms, :millisecond)

    case heartbeat_history(since) do
      {:ok, rows} ->
        seeded =
          Enum.reduce(rows, state.heartbeats, fn [node_id, at, ready], acc ->
            beat = {DateTime.to_unix(at, :millisecond), ready}
            Map.update(acc, "pop.#{node_id}", [beat], &[beat | &1])
          end)
          |> Map.new(fn {key, beats} ->
            {key, beats |> Enum.uniq() |> Enum.sort_by(&elem(&1, 0), :desc)}
          end)

        {:noreply, broadcast(%{state | heartbeats: seeded, started_at: now - @heartbeat_keep_ms})}

      {:error, reason} ->
        Logger.debug("heartbeat history unavailable, retrying: #{inspect(reason)}")
        Process.send_after(self(), :seed_heartbeats, @retry_after)

        {:noreply, state}
    end
  end

  def handle_info({:EXIT, pid, reason}, %{watchers: watchers} = state) do
    if pid in watchers do
      Logger.info("kv watcher stopped (#{inspect(reason)}), rewatching")
      Enum.each(watchers -- [pid], &KV.unwatch/1)
      Process.send_after(self(), :watch, @retry_after)

      {:noreply, Map.merge(state, %{connected: false, watchers: []})}
    else
      {:noreply, state}
    end
  end

  def handle_info({:kv, :control, :key_added, _key, value}, state) do
    case Jason.decode(value) do
      {:ok, decoded} ->
        {:noreply, broadcast(Map.put(state, :control, decoded))}

      {:error, reason} ->
        Logger.warning("undecodable control snapshot: #{inspect(reason)}")

        {:noreply, state}
    end
  end

  def handle_info({:kv, :control, action, _key, _value}, state)
      when action in [:key_deleted, :key_purged] do
    {:noreply, broadcast(Map.put(state, :control, nil))}
  end

  def handle_info({:kv, :states, :key_added, key, value}, state)
      when is_binary(key) and binary_part(key, byte_size(key) - 9, 9) == ".topology" do
    with [prefix, id, "topology"] <- String.split(key, "."),
         {:ok, decoded} <- Jason.decode(value) do
      now = System.system_time(:millisecond)
      beat = {now, decoded["ready"] == true}

      heartbeats =
        Map.update(state.heartbeats, "#{prefix}.#{id}", [beat], fn beats ->
          [beat | Enum.take_while(beats, fn {at, _} -> now - at <= @heartbeat_keep_ms end)]
        end)

      {:noreply, broadcast(%{state | heartbeats: heartbeats})}
    else
      _ -> {:noreply, state}
    end
  end

  def handle_info({:kv, bucket, :key_added, key, value}, state) do
    with {:ok, node_key} <- node_key(bucket, key),
         {:ok, decoded} <- Jason.decode(value) do
      {:noreply,
       broadcast(
         Map.update(state, bucket, %{node_key => decoded}, &Map.put(&1, node_key, decoded))
       )}
    else
      :other_lane ->
        {:noreply, state}

      {:error, reason} ->
        Logger.warning("undecodable #{bucket} value for #{key}: #{inspect(reason)}")

        {:noreply, state}
    end
  end

  def handle_info({:kv, bucket, action, key, _value}, state)
      when action in [:key_deleted, :key_purged] do
    case node_key(bucket, key) do
      {:ok, node_key} ->
        {:noreply, broadcast(Map.update(state, bucket, %{}, &Map.delete(&1, node_key)))}

      :other_lane ->
        {:noreply, state}
    end
  end

  def handle_info(_message, state), do: {:noreply, state}

  # keys are <prefix>.<id>.<lane>; the rest of the pane keys nodes by <prefix>.<id>
  def node_key(bucket, key) do
    lane = lane_for(bucket)

    case String.split(key, ".") do
      [prefix, id, ^lane] -> {:ok, "#{prefix}.#{id}"}
      _ -> :other_lane
    end
  end

  defp lane_for(:intents), do: "intent"
  defp lane_for(:states), do: "dataplane"

  defp start_watchers do
    owner = self()

    with :ok <- connection_ready(),
         {:ok, intents} <- watch(@intents_bucket, owner, :intents),
         {:ok, states} <- watch(@state_bucket, owner, :states),
         {:ok, control} <- watch(@control_bucket, owner, :control) do
      {:ok, [intents, states, control]}
    else
      {:error, reason} -> {:error, reason}
    end
  end

  defp connection_ready do
    case Process.whereis(MaetoPane.Nats.connection()) do
      nil -> {:error, :nats_unavailable}
      pid -> if Process.alive?(pid), do: :ok, else: {:error, :nats_unavailable}
    end
  end

  defp watch(bucket, owner, tag) do
    KV.watch(MaetoPane.Nats.connection(), bucket, fn action, key, value ->
      send(owner, {:kv, tag, action, key, value})
    end)
  rescue
    error -> {:error, error}
  catch
    :exit, reason -> {:error, reason}
  end

  defp describe({{:badmatch, {:error, %{"description" => description}}}, _stack}), do: description

  defp describe({{:badmatch, {:error, reason}}, _stack}), do: inspect(reason)

  defp describe(reason), do: inspect(reason)

  defp heartbeat_history(since) do
    sql = "SELECT node_id, time, ready FROM node_heartbeats WHERE time >= $1 ORDER BY time"

    case Postgrex.query(MaetoPane.DB, sql, [since]) do
      {:ok, result} -> {:ok, result.rows}
      {:error, error} -> {:error, error}
    end
  catch
    :exit, reason -> {:error, reason}
  end

  defp broadcast(state) do
    Phoenix.PubSub.broadcast(MaetoPane.PubSub, @topic, :pane_updated)

    state
  end
end
