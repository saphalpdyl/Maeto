defmodule MaetoPaneWeb.PopLive do
  @moduledoc """
  One PoP, every counter. The PoP's inspector on the left, its total throughput
  across the core links on top, then a matrix with one row per device the
  agent reads and one column per counter. Hovering any chart moves all of
  them to the same moment.
  """

  use MaetoPaneWeb, :live_view

  alias MaetoPane.Pane
  alias MaetoPane.Pane.Analysis
  alias MaetoPane.Pane.IfaceStats
  alias MaetoPane.Pane.Links
  alias MaetoPane.Pane.Paths
  alias MaetoPaneWeb.Pane.Nav
  alias MaetoPaneWeb.Pane.RateScale

  import MaetoPaneWeb.Pane.Inspector.NodePanel
  import MaetoPaneWeb.Pane.Nav
  import MaetoPaneWeb.Pane.SyncChart
  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  @refresh_ms 10_000

  @columns [
    {:bits, "bits", :rx_bytes, :tx_bytes},
    {:packets, "packets", :rx_packets, :tx_packets},
    {:drops, "drops", :rx_dropped, :tx_dropped}
  ]

  @impl true
  def mount(%{"id" => id}, _session, socket) do
    if connected?(socket) do
      put_tz_offset(get_connect_params(socket)["tz_offset"])
      Pane.subscribe()
      :timer.send_interval(@refresh_ms, :refresh_stats)
    end

    {:ok,
     socket
     |> assign(:id, id)
     |> assign(:columns, @columns)
     |> load_node()
     |> load_stats()}
  end

  @impl true
  def handle_event("clear", _params, socket), do: {:noreply, push_navigate(socket, to: ~p"/")}

  @impl true
  def handle_info(:pane_updated, socket), do: {:noreply, load_node(socket)}
  def handle_info(:refresh_stats, socket), do: {:noreply, load_stats(socket)}

  defp load_node(socket) do
    snapshot = Pane.snapshot()
    graph = Analysis.graph(snapshot)
    node = Enum.find(graph.nodes, &(&1.id == socket.assigns.id))

    socket
    |> assign(:node, node)
    |> assign(:graph_nodes, graph.nodes)
    |> assign(:links, Links.from_graph(graph))
    |> assign(:paths, Paths.rows(snapshot))
    |> assign(:nav, Nav.groups(nil, graph.nodes))
    |> assign(:peers, peers(graph, socket.assigns.id))
    |> assign(:access_iface, access_interface(node))
    |> assign(:connected_to_nats, snapshot.connected)
    |> assign(:domain, graph.domain["locator_prefix"])
    |> assign(:page_title, (node && node.name) || socket.assigns.id)
  end

  defp load_stats(socket) do
    assign(socket, :stats, IfaceStats.for_node(socket.assigns.id, Map.keys(socket.assigns.peers)))
  end

  # which neighbour each core interface faces, so a row reads "to PopB"
  defp peers(graph, id) do
    names = Map.new(graph.nodes, &{&1.id, &1.name || &1.id})

    Enum.reduce(graph.links, %{}, fn link, acc ->
      Enum.reduce(link.edges, acc, fn edge, acc ->
        cond do
          edge["local"] == id ->
            Map.put(acc, edge["local_iface"], Map.get(names, edge["remote"], edge["remote"]))

          edge["remote"] == id ->
            Map.put(acc, edge["remote_iface"], Map.get(names, edge["local"], edge["local"]))

          true ->
            acc
        end
      end)
    end)
  end

  defp access_interface(%{inventory: %{"access" => %{"interface" => iface}}}), do: iface
  defp access_interface(_node), do: nil

  defp device_label(iface, peers, access_iface) do
    cond do
      iface == access_iface -> "access"
      Map.has_key?(peers, iface) -> "to #{Map.get(peers, iface)}"
      String.starts_with?(iface, "maeto-tun-") -> "tunnel · tenant #{tenant_of(iface)}"
      String.starts_with?(iface, "maeto-vrf-") -> "VRF · tenant #{tenant_of(iface)}"
      true -> nil
    end
  end

  defp tenant_of("maeto-tun-" <> rest), do: rest |> String.split("-") |> List.first()
  defp tenant_of("maeto-vrf-" <> rest), do: rest

  defp cells(series, columns) do
    Enum.map(columns, fn {kind, _label, rx_stat, tx_stat} ->
      {[rx, tx], unit} = scaled(kind, Map.fetch!(series, rx_stat), Map.fetch!(series, tx_stat))
      %{kind: kind, rx: rx, tx: tx, unit: unit}
    end)
  end

  defp scaled(:bits, rx, tx), do: RateScale.bits([rx, tx])
  defp scaled(:packets, rx, tx), do: RateScale.packets([rx, tx])
  defp scaled(:drops, rx, tx), do: RateScale.events([rx, tx])

  defp total_chart(stats) do
    {[rx, tx], unit} = RateScale.bits([stats.total.rx_bits, stats.total.tx_bits])
    %{rx: rx, tx: tx, unit: unit}
  end

  defp dom_id(iface, stat), do: "chart-#{iface}-#{stat}"

  @impl true
  def render(assigns) do
    ~H"""
    <Layouts.app flash={@flash}>
      <:rail>
        <.nav groups={@nav} mode={:navigate} active={"pop-#{@id}"} />
      </:rail>

      <:foot>
        <div :if={@domain} class="px-1.5 font-mono text-[10.5px] text-faint">{@domain}</div>
      </:foot>

      <:crumbs>
        <span class="shrink-0 text-[13px] text-muted">Control plane</span>
        <span class="shrink-0 text-faint">&rsaquo;</span>
        <.link navigate={~p"/"} class="shrink-0 text-[13px] text-muted hover:text-ink">Pane</.link>
        <span class="shrink-0 text-faint">&rsaquo;</span>
        <span class="shrink-0 text-[13px] font-medium">{(@node && @node.name) || @id}</span>
      </:crumbs>

      <:status>
        <.pill tone={if @connected_to_nats, do: "ok", else: "warn"}>
          {if @connected_to_nats, do: "Live", else: "Reconnecting"}
        </.pill>
      </:status>

      <:inspector>
        <.node_panel
          :if={@node}
          selected={@node}
          paths={@paths}
          links={@links}
          nodes={@graph_nodes}
          drilldown
        />
        <p :if={!@node} class="px-4 py-4 text-[13px] text-muted">No PoP with id {@id}</p>
      </:inspector>

      <div id="pop-charts" phx-hook="ChartSync">
        <header class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-line px-6 py-4">
          <h1 class="text-[15px] font-medium">{(@node && @node.name) || @id} counters</h1>
          <.pill :if={@node} tone={node_tone(@node.status)}>{to_string(@node.status)}</.pill>
          <span :if={@stats} class="ml-auto font-mono text-xs text-muted">
            {length(@stats.devices)} devices · last 30 min · 30s buckets
          </span>
        </header>

        <p :if={@stats == nil} class="px-6 py-10 text-[13px] text-muted">No counter history</p>

        <%= if @stats do %>
          <section class="border-b border-line px-6 py-5">
            <.wide
              id="chart-total"
              title="Throughput"
              solid={total_chart(@stats).rx}
              dotted={total_chart(@stats).tx}
              unit={total_chart(@stats).unit}
              from={@stats.from}
              solid_label="in, all core links"
              dotted_label="out, all core links"
            />
          </section>

          <section class="px-6 py-5">
            <div class="grid grid-cols-[160px_repeat(3,minmax(0,1fr))] gap-x-4 gap-y-4">
              <div></div>
              <div
                :for={{_kind, label, _rx, _tx} <- @columns}
                class="flex items-center gap-3 text-xs font-medium text-muted"
              >
                {label}
                <span class="flex items-center gap-1 font-normal text-faint">
                  <span class="h-0.5 w-3 rounded-full bg-ok" />rx
                </span>
                <span class="flex items-center gap-1 font-normal text-faint">
                  <span class="flex w-3 justify-between">
                    <span class="size-[2.5px] rounded-full bg-steer" />
                    <span class="size-[2.5px] rounded-full bg-steer" />
                    <span class="size-[2.5px] rounded-full bg-steer" />
                  </span>
                  tx
                </span>
              </div>

              <%= for {iface, series} <- @stats.devices do %>
                <div class="min-w-0 pt-1">
                  <div class="truncate font-mono text-[13px]">{iface}</div>
                  <div
                    :if={device_label(iface, @peers, @access_iface)}
                    class="truncate text-[11px] text-muted"
                  >
                    {device_label(iface, @peers, @access_iface)}
                  </div>
                </div>
                <.cell
                  :for={cell <- cells(series, @columns)}
                  id={dom_id(iface, cell.kind)}
                  rx={cell.rx}
                  tx={cell.tx}
                  unit={cell.unit}
                  from={@stats.from}
                />
              <% end %>
            </div>

            <p :if={@stats.devices == []} class="py-6 text-[13px] text-muted">
              The agent has not reported any counters for this PoP yet
            </p>
          </section>
        <% end %>
      </div>
    </Layouts.app>
    """
  end
end
