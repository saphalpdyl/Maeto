defmodule MaetoPane.Fabric do
  @moduledoc "Latest desired intent and observed dataplane state for every node."

  use GenServer

  require Logger

  alias Gnat.Jetstream.API.KV

  @intents_bucket "maeto-intents"
  @state_bucket "maeto-state"
  @control_bucket "maeto-control"
  @topic "fabric"
  @retry_after 2_000

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def snapshot, do: GenServer.call(__MODULE__, :snapshot)

  def subscribe, do: Phoenix.PubSub.subscribe(MaetoPane.PubSub, @topic)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    send(self(), :watch)

    {:ok, empty()}
  end

  @impl true
  def handle_call(:snapshot, _from, state) do
    {:reply, Map.merge(empty(), state), state}
  end

  defp empty do
    %{intents: %{}, states: %{}, control: nil, connected: false, watchers: []}
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

  def handle_info({:kv, bucket, :key_added, key, value}, state) do
    case Jason.decode(value) do
      {:ok, decoded} ->
        {:noreply,
         broadcast(Map.update(state, bucket, %{key => decoded}, &Map.put(&1, key, decoded)))}

      {:error, reason} ->
        Logger.warning("undecodable #{bucket} value for #{key}: #{inspect(reason)}")

        {:noreply, state}
    end
  end

  def handle_info({:kv, bucket, action, key, _value}, state)
      when action in [:key_deleted, :key_purged] do
    {:noreply, broadcast(Map.update(state, bucket, %{}, &Map.delete(&1, key)))}
  end

  def handle_info(_message, state), do: {:noreply, state}

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

  defp broadcast(state) do
    Phoenix.PubSub.broadcast(MaetoPane.PubSub, @topic, :fabric_updated)

    state
  end
end
