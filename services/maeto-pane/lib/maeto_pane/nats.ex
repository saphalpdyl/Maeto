defmodule MaetoPane.Nats do
  @moduledoc "Supervised NATS connection settings, driven by NATS_CONNECT_URL."

  @connection :maeto_nats

  def connection, do: @connection

  def child_spec(_opts) do
    settings = %{
      name: @connection,
      backoff_period: 2_000,
      connection_settings: [connection_settings()]
    }

    %{
      id: __MODULE__,
      start: {Gnat.ConnectionSupervisor, :start_link, [settings, [name: __MODULE__]]},
      type: :worker
    }
  end

  def connection_settings do
    uri = URI.parse(Application.get_env(:maeto_pane, :nats_url, "nats://127.0.0.1:4222"))

    base = %{host: uri.host || "127.0.0.1", port: uri.port || 4222}

    if Application.get_env(:maeto_pane, :nats_inet6, false) do
      Map.put(base, :tcp_opts, [:inet6, :binary])
    else
      base
    end
  end
end
