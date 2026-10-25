defmodule MaetoPaneWeb.PaneLive do
  @moduledoc """
  The pane console.

  Table-first, with the topology as a persistent orientation strip and a side
  inspector. Follows the causal chain in Maeto.design.md -- link condition ->
  cost -> computed path -> segment list -> programmed dataplane -> observed
  state -- in the visual language of the Aether console.
  """

  use MaetoPaneWeb, :live_view

  alias MaetoPane.Pane
  alias MaetoPane.Pane.Analysis
  alias MaetoPane.Pane.Paths
  alias MaetoPane.Pane.Links
  alias MaetoPane.Pane.IfaceStats
  alias MaetoPane.Pane.LinkStats
  alias MaetoPaneWeb.Pane.RateScale

  import MaetoPaneWeb.Pane.ChangesTable
  import MaetoPaneWeb.Pane.ComputedTable
  import MaetoPaneWeb.Pane.Inspector
  import MaetoPaneWeb.Pane.LinksTable
  import MaetoPaneWeb.Pane.Nav

  alias MaetoPaneWeb.Pane.Nav
  import MaetoPaneWeb.Pane.PathsTable
  import MaetoPaneWeb.Pane.RegistryPanel
  import MaetoPaneWeb.Pane.SitesTable
  import MaetoPaneWeb.Pane.Reconciliation
  import MaetoPaneWeb.Pane.SyncChart
  import MaetoPaneWeb.Pane.Topology
  import MaetoPaneWeb.Pane.UI

  @overlays [health: "Health", cost: "Cost", path: "Path"]
  @stats_refresh_ms 10_000

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket) do
      put_tz_offset(get_connect_params(socket)["tz_offset"])
      Pane.subscribe()
      :timer.send_interval(@stats_refresh_ms, :refresh_stats)
    end

    {:ok,
     socket
     |> assign(:selection, nil)
     |> assign(:link_stats, nil)
     |> assign(:section, :paths)
     |> assign(:overlay, :health)
     |> assign(:overlays, @overlays)
     |> assign(:page_title, "Pane")
     |> assign(:fabric, nil)
     |> load()
     |> load_fabric()}
  end

  @impl true
  def handle_params(%{"section" => section}, _uri, socket)
      when section in ~w(changes paths links computed sites registry) do
    {:noreply, assign(socket, :section, String.to_existing_atom(section))}
  end

  def handle_params(_params, _uri, socket), do: {:noreply, socket}

  @impl true
  def handle_info(:pane_updated, socket), do: {:noreply, load(socket)}

  def handle_info(:refresh_stats, socket),
    do: {:noreply, socket |> load_link_stats() |> load_fabric()}

  @impl true
  def handle_event("select", %{"kind" => kind, "id" => id}, socket)
      when kind in ~w(node link path site) do
    # a segment list is what the path overlay exists to draw, so reach for it
    overlay =
      if kind == "path" and socket.assigns.overlay == :health,
        do: :path,
        else: socket.assigns.overlay

    {:noreply,
     socket
     |> assign(:selection, {String.to_existing_atom(kind), id})
     |> assign(:overlay, overlay)
     |> resolve()
     |> load_link_stats()}
  end

  def handle_event("clear", _params, socket) do
    {:noreply, socket |> assign(:selection, nil) |> assign(:link_stats, nil) |> resolve()}
  end

  def handle_event("overlay", %{"overlay" => overlay}, socket)
      when overlay in ~w(health cost path) do
    {:noreply, assign(socket, :overlay, String.to_existing_atom(overlay))}
  end

  defp load(socket) do
    snapshot = Pane.snapshot()

    nodes = Analysis.nodes(snapshot)
    registry = Analysis.registry(snapshot)
    graph = Analysis.graph(snapshot)
    rows = Paths.rows(snapshot)
    sites = Paths.sites(snapshot)
    links = Links.from_graph(graph)
    computed = Paths.computed(snapshot)
    changes = Paths.changes(snapshot)

    socket
    |> assign(:connected_to_nats, snapshot.connected)
    |> assign(:has_control, not is_nil(snapshot.control))
    |> assign(:graph, graph)
    |> assign(:access, access_interfaces(graph))
    |> assign(:registry, registry)
    |> assign(:paths, rows)
    |> assign(:sites, sites)
    |> assign(:links, links)
    |> assign(:computed, computed)
    |> assign(:changes, changes)
    |> assign(:sites_by_node, Enum.frequencies_by(sites, & &1.attach))
    |> assign(:issues, Analysis.issues(nodes) ++ Analysis.drift(registry) ++ Paths.issues(rows))
    |> assign(:facts, facts(graph, links, sites, rows, computed, changes))
    |> then(&assign(&1, :nav, Nav.groups(&1.assigns.facts, graph.nodes)))
    |> assign(:programmed_pairs, MapSet.new(rows, &{&1.pe, &1.dest}))
    |> assign(:ceiling, Enum.max([1.0 | Enum.map(links, & &1.cost)]))
    |> resolve()
  end

  # every PE names the interface its CPEs attach to; transit PoPs have none
  defp access_interfaces(graph) do
    Enum.flat_map(graph.nodes, fn node ->
      case access_interface(node) do
        nil -> []
        iface -> [{node.id, iface}]
      end
    end)
  end

  defp access_interface(%{inventory: %{"access" => %{"interface" => iface}}}), do: iface
  defp access_interface(_node), do: nil

  defp load_fabric(socket) do
    if connected?(socket) do
      assign(socket, :fabric, IfaceStats.fabric(socket.assigns.access))
    else
      socket
    end
  end

  defp fabric_chart(fabric) do
    {[rx, tx], unit} = RateScale.bits([fabric.rx_bits, fabric.tx_bits])
    %{rx: rx, tx: tx, unit: unit}
  end

  defp load_link_stats(%{assigns: %{selected: %{kind: :link} = link}} = socket),
    do: assign(socket, :link_stats, LinkStats.for_link(link))

  defp load_link_stats(socket), do: assign(socket, :link_stats, nil)

  defp resolve(socket) do
    %{graph: graph, paths: paths, sites: sites} = socket.assigns

    selected =
      case socket.assigns.selection do
        {:path, id} -> Paths.select(paths, id)
        {:site, id} -> Enum.find(sites, &(&1.portal_id == id))
        selection -> Analysis.select(graph, selection)
      end

    socket
    |> assign(:selected, selected)
    |> assign(:trace, trace(selected))
  end

  defp trace(%{kind: :path} = row),
    do: %{nodes: Paths.chain(row), pairs: MapSet.new(Paths.hop_pairs(row))}

  defp trace(_selected), do: %{nodes: [], pairs: MapSet.new()}

  defp facts(graph, links, sites, rows, computed, changes) do
    members = Enum.flat_map(graph.links, & &1.edges)
    site_counts = Paths.site_counts(sites)
    path_counts = Paths.path_counts(rows)

    pop_states = Enum.frequencies_by(graph.nodes, & &1.status)

    %{
      pops: length(graph.nodes),
      pops_ok: Map.get(pop_states, :ok, 0),
      pops_converging: Map.get(pop_states, :converging, 0),
      pops_failing:
        length(graph.nodes) - Map.get(pop_states, :ok, 0) - Map.get(pop_states, :converging, 0),
      domain: graph.domain["locator_prefix"],
      adjacencies: length(members),
      adjacencies_up: Enum.count(members, & &1["up"]),
      links: length(links),
      sites: site_counts.total,
      sites_up: site_counts.up,
      paths: path_counts.total,
      paths_installed: path_counts.installed,
      paths_degraded: path_counts.degraded,
      paths_pending: Enum.count(rows, &(&1.status == :pending)),
      computed: length(computed),
      changes: length(changes)
    }
  end

  defp section_title(:changes), do: "Path changes"
  defp section_title(:paths), do: "Segment lists"
  defp section_title(:computed), do: "Computed paths"
  defp section_title(:links), do: "Links"
  defp section_title(:sites), do: "Sites"
  defp section_title(:registry), do: "Service registry"

  defp section_meta(:paths, facts), do: "#{facts.paths_installed}/#{facts.paths} programmed"
  defp section_meta(:links, facts), do: "#{facts.adjacencies_up}/#{facts.adjacencies} members up"
  defp section_meta(:computed, facts), do: "#{facts.computed} paths"
  defp section_meta(:sites, facts), do: "#{facts.sites_up}/#{facts.sites} reporting"
  defp section_meta(:changes, facts), do: "#{facts.changes} events"
  defp section_meta(:registry, _facts), do: nil

  defp section_caption(section, _facts, _registry) when section != :registry, do: nil

  defp section_caption(:registry, _facts, registry) do
    if registry.present?,
      do: "SID cursor #{registry.cursor}, #{length(registry.allocated)} allocated",
      else: "not published"
  end

  @impl true
  def render(assigns) do
    ~H"""
    <Layouts.app flash={@flash}>
      <:rail>
        <.nav groups={@nav} active={@section} />
      </:rail>

      <:foot>
        <div :if={@facts.domain} class="px-1.5 font-mono text-[10.5px] text-faint">
          {@facts.domain} · {@facts.pops} PoPs
        </div>
      </:foot>

      <:crumbs>
        <span class="shrink-0 text-[13px] text-muted">Control plane</span>
        <span class="shrink-0 text-faint">&rsaquo;</span>
        <span class="shrink-0 text-[13px] font-medium">Pane</span>
        <span
          :if={@facts.domain}
          class="ml-1 shrink-0 rounded-[5px] border border-line bg-surface px-1.5 py-px font-mono text-[11px] text-muted"
        >
          {@facts.domain}
        </span>
      </:crumbs>

      <:status>
        <.pill tone={if @connected_to_nats, do: "ok", else: "warn"}>
          {if @connected_to_nats, do: "Live", else: "Reconnecting"}
        </.pill>
        <span class="hidden font-mono text-xs text-muted sm:inline">
          {if @has_control, do: clock(@graph.published_at), else: "no snapshot"}
        </span>
      </:status>

      <:inspector>
        <.panel
          selected={@selected}
          issues={@issues}
          computed={@computed}
          paths={@paths}
          links={@links}
          nodes={@graph.nodes}
          link_stats={@link_stats}
        />
      </:inspector>

      <div phx-window-keyup="clear" phx-key="Escape">
        <h1 class="sr-only">Pane</h1>
        <section class="grid grid-cols-1 border-b border-line xl:grid-cols-[minmax(0,1fr)_360px]">
          <div id="fabric-chart" phx-hook="ChartSync" class="min-w-0 px-6 py-5">
            <.wide
              :if={@fabric}
              id="chart-fabric"
              title="Fabric throughput"
              solid={fabric_chart(@fabric).rx}
              dotted={fabric_chart(@fabric).tx}
              unit={fabric_chart(@fabric).unit}
              from={@fabric.from}
              solid_label="in from sites"
              dotted_label="out to sites"
              height="h-[140px]"
            />
            <p :if={!@fabric} class="text-[13px] text-muted">No counter history</p>
          </div>
          <div class="border-t border-line xl:border-t-0 xl:border-l">
            <.reconciliation facts={@facts} />
          </div>
        </section>

        <.topology
          graph={@graph}
          links={@links}
          sites_by_node={@sites_by_node}
          overlay={@overlay}
          overlays={@overlays}
          trace={@trace}
          selection={@selection}
          ceiling={@ceiling}
        />

        <section>
          <div class="flex flex-wrap items-center gap-x-2.5 gap-y-1 px-6 pt-5 pb-3">
            <h2 class="text-[15px] font-medium">{section_title(@section)}</h2>
            <p
              :if={section_caption(@section, @facts, @registry)}
              class="text-[13px] text-faint"
            >
              {section_caption(@section, @facts, @registry)}
            </p>
            <span
              :if={section_meta(@section, @facts)}
              class="ml-auto font-mono text-xs text-muted"
            >
              {section_meta(@section, @facts)}
            </span>
          </div>

          <div class="mt-scroll overflow-x-auto border-t border-line px-2">
            <%= case @section do %>
              <% :changes -> %>
                <.changes_table changes={@changes} selection={@selection} />
              <% :paths -> %>
                <.paths_table paths={@paths} selection={@selection} />
              <% :links -> %>
                <.links_table links={@links} selection={@selection} />
              <% :computed -> %>
                <.computed_table computed={@computed} programmed={@programmed_pairs} />
              <% :sites -> %>
                <.sites_table sites={@sites} selection={@selection} />
              <% :registry -> %>
                <.registry_panel registry={@registry} />
            <% end %>
          </div>
        </section>
      </div>
    </Layouts.app>
    """
  end
end
