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
  @path_change_keep 1_024

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def snapshot, do: GenServer.call(__MODULE__, :snapshot)

  def subscribe, do: Phoenix.PubSub.subscribe(MaetoPane.PubSub, @topic)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    send(self(), :watch)
    send(self(), :seed_heartbeats)
    send(self(), :seed_path_changes)

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
      repairing: %{},
      path_changes: [],
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
    owner = self()
    since = DateTime.add(DateTime.utc_now(), -@heartbeat_keep_ms, :millisecond)

    Task.start(fn -> send(owner, {:heartbeat_history, heartbeat_history(since)}) end)

    {:noreply, state}
  end

  def handle_info({:heartbeat_history, {:ok, rows}}, state) do
    now = System.system_time(:millisecond)

    seeded =
      Enum.reduce(rows, state.heartbeats, fn [node_id, at, ready], acc ->
        beat = {DateTime.to_unix(at, :millisecond), ready}
        Map.update(acc, "pop.#{node_id}", [beat], &[beat | &1])
      end)
      |> Map.new(fn {key, beats} ->
        {key, beats |> Enum.uniq() |> Enum.sort_by(&elem(&1, 0), :desc)}
      end)

    started_at =
      case rows do
        [[_node_id, first, _ready] | _] -> min(DateTime.to_unix(first, :millisecond), now)
        [] -> state.started_at
      end

    {:noreply, broadcast(%{state | heartbeats: seeded, started_at: started_at})}
  end

  def handle_info({:heartbeat_history, {:error, reason}}, state) do
    Logger.debug("heartbeat history unavailable, retrying: #{inspect(reason)}")
    Process.send_after(self(), :seed_heartbeats, @retry_after)

    {:noreply, state}
  end

  def handle_info(:seed_path_changes, state) do
    owner = self()

    Task.start(fn -> send(owner, {:path_change_history, path_change_history()}) end)

    {:noreply, state}
  end

  def handle_info({:path_change_history, {:ok, rows}}, state) do
    history = Enum.map(rows, &path_change_row/1)

    {:noreply,
     broadcast(%{state | path_changes: merge_path_changes(state.path_changes, history)})}
  end

  def handle_info({:path_change_history, {:error, reason}}, state) do
    Logger.debug("path change history unavailable, retrying: #{inspect(reason)}")
    Process.send_after(self(), :seed_path_changes, @retry_after)

    {:noreply, state}
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
        path_changes = merge_path_changes(state.path_changes, decoded["path_changes"] || [])

        {:noreply, broadcast(Map.merge(state, %{control: decoded, path_changes: path_changes}))}

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

      node_key = "#{prefix}.#{id}"

      heartbeats =
        Map.update(state.heartbeats, node_key, [beat], fn beats ->
          [beat | Enum.take_while(beats, fn {at, _} -> now - at <= @heartbeat_keep_ms end)]
        end)

      repairing = Map.put(state.repairing, node_key, decoded["repairing"] || [])

      {:noreply, broadcast(%{state | heartbeats: heartbeats, repairing: repairing})}
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

  defp path_change_history do
    sql = """
    SELECT time, tenant, label, src, dst, kind, from_nodes, to_nodes, from_cost, to_cost, causes
    FROM path_changes
    ORDER BY time DESC
    LIMIT $1
    """

    case Postgrex.query(MaetoPane.DB, sql, [@path_change_keep]) do
      {:ok, result} -> {:ok, result.rows}
      {:error, error} -> {:error, error}
    end
  catch
    :exit, reason -> {:error, reason}
  end

  defp path_change_row([
         at,
         tenant,
         label,
         src,
         dst,
         kind,
         from_nodes,
         to_nodes,
         from_cost,
         to_cost,
         causes
       ]) do
    %{
      "at" => DateTime.to_iso8601(at),
      "tenant" => tenant,
      "label" => label,
      "src" => src,
      "dst" => dst,
      "kind" => kind,
      "from_path" => %{"nodes" => from_nodes, "cost" => from_cost},
      "to_path" => %{"nodes" => to_nodes, "cost" => to_cost},
      "causes" => causes
    }
  end

  defp merge_path_changes(kept, incoming) do
    (incoming ++ kept)
    |> Enum.uniq_by(&path_change_key/1)
    |> Enum.sort_by(&path_change_time/1, :desc)
    |> Enum.take(@path_change_keep)
  end

  defp path_change_key(change) do
    {path_change_time(change), change["tenant"], change["label"], change["src"], change["dst"]}
  end

  defp path_change_time(change) do
    case DateTime.from_iso8601(change["at"] || "") do
      {:ok, at, _offset} -> DateTime.to_unix(at, :microsecond)
      _ -> 0
    end
  end

  defp broadcast(state) do
    Phoenix.PubSub.broadcast(MaetoPane.PubSub, @topic, :pane_updated)

    state
  end
end
