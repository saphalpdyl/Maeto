defmodule MaetoPane.Sim do
  @moduledoc """
  The testbed simulator as the pane sees it: the latest state maeto-sim
  published to its KV bucket, and a way to send it commands. Kept apart from
  `MaetoPane.Pane` so a missing simulator never stalls the control plane view.
  """

  use GenServer

  require Logger

  alias Gnat.Jetstream.API.KV

  @bucket "maeto-sim"
  @key "state"
  @subject "maeto.sim.cmd"
  @topic "sim"
  @retry_after 3_000
  @request_timeout 10_000

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def snapshot, do: GenServer.call(__MODULE__, :snapshot)

  def subscribe, do: Phoenix.PubSub.subscribe(MaetoPane.PubSub, @topic)

  def command(request) do
    case Gnat.request(MaetoPane.Nats.connection(), @subject, Jason.encode!(request),
           receive_timeout: @request_timeout
         ) do
      {:ok, %{body: body}} -> decode_reply(body)
      {:error, :timeout} -> {:error, "the simulator did not answer"}
      {:error, :no_responders} -> {:error, "the simulator is not running"}
      {:error, reason} -> {:error, inspect(reason)}
    end
  catch
    :exit, _reason -> {:error, "not connected to NATS"}
  end

  defp decode_reply(body) do
    case Jason.decode(body) do
      {:ok, %{"ok" => true}} -> :ok
      {:ok, %{"error" => error}} -> {:error, error}
      _ -> {:error, "unreadable reply from the simulator"}
    end
  end

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    send(self(), :watch)

    {:ok, %{state: nil, watcher: nil}}
  end

  @impl true
  def handle_call(:snapshot, _from, state), do: {:reply, state.state, state}

  @impl true
  def handle_info(:watch, state) do
    case watch() do
      {:ok, watcher} ->
        {:noreply, %{state | watcher: watcher}}

      {:error, reason} ->
        Logger.debug("sim kv watch unavailable, retrying: #{inspect(reason)}")
        Process.send_after(self(), :watch, @retry_after)

        {:noreply, %{state | watcher: nil}}
    end
  end

  def handle_info({:EXIT, pid, reason}, %{watcher: pid} = state) do
    Logger.info("sim kv watcher stopped (#{inspect(reason)}), rewatching")
    Process.send_after(self(), :watch, @retry_after)

    {:noreply, %{state | watcher: nil}}
  end

  def handle_info({:sim, :key_added, @key, value}, state) do
    case Jason.decode(value) do
      {:ok, decoded} ->
        {:noreply, broadcast(%{state | state: decoded})}

      {:error, reason} ->
        Logger.warning("undecodable sim state: #{inspect(reason)}")

        {:noreply, state}
    end
  end

  def handle_info({:sim, action, @key, _value}, state)
      when action in [:key_deleted, :key_purged] do
    {:noreply, broadcast(%{state | state: nil})}
  end

  def handle_info(_message, state), do: {:noreply, state}

  defp watch do
    owner = self()

    case Process.whereis(MaetoPane.Nats.connection()) do
      nil ->
        {:error, :nats_unavailable}

      _pid ->
        KV.watch(MaetoPane.Nats.connection(), @bucket, fn action, key, value ->
          send(owner, {:sim, action, key, value})
        end)
    end
  rescue
    error -> {:error, error}
  catch
    :exit, reason -> {:error, reason}
  end

  defp broadcast(state) do
    Phoenix.PubSub.broadcast(MaetoPane.PubSub, @topic, :sim_updated)

    state
  end
end
