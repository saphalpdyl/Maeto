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

  import MaetoPaneWeb.Pane.ChangesTable
  import MaetoPaneWeb.Pane.ComputedTable
  import MaetoPaneWeb.Pane.Inspector
  import MaetoPaneWeb.Pane.LinksTable
  import MaetoPaneWeb.Pane.Nav
  import MaetoPaneWeb.Pane.PathsTable
  import MaetoPaneWeb.Pane.RegistryPanel
  import MaetoPaneWeb.Pane.SitesTable
  import MaetoPaneWeb.Pane.Summary
  import MaetoPaneWeb.Pane.Topology
  import MaetoPaneWeb.Pane.UI

  @overlays [health: "Health", cost: "Cost", path: "Path"]

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket), do: Pane.subscribe()

    {:ok,
     socket
     |> assign(:selection, nil)
     |> assign(:section, :paths)
     |> assign(:overlay, :health)
     |> assign(:overlays, @overlays)
     |> assign(:page_title, "Pane")
     |> load()}
  end

  @impl true
  def handle_info(:pane_updated, socket), do: {:noreply, load(socket)}

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
     |> resolve()}
  end

  def handle_event("clear", _params, socket) do
    {:noreply, socket |> assign(:selection, nil) |> resolve()}
  end

  def handle_event("section", %{"section" => section}, socket)
      when section in ~w(changes paths links computed sites registry) do
    {:noreply, assign(socket, :section, String.to_existing_atom(section))}
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
    |> assign(:registry, registry)
    |> assign(:paths, rows)
    |> assign(:sites, sites)
    |> assign(:links, links)
    |> assign(:computed, computed)
    |> assign(:changes, changes)
    |> assign(:sites_by_node, Enum.frequencies_by(sites, & &1.attach))
    |> assign(:issues, Analysis.issues(nodes) ++ Analysis.drift(registry) ++ Paths.issues(rows))
    |> assign(:facts, facts(graph, links, sites, rows, computed, changes))
    |> then(&assign(&1, :nav, nav_groups(&1.assigns.facts)))
    |> assign(:programmed_pairs, MapSet.new(rows, &{&1.pe, &1.dest}))
    |> assign(:ceiling, Enum.max([1.0 | Enum.map(links, & &1.cost)]))
    |> resolve()
  end

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

  defp nav_groups(facts) do
    [
      %{
        label: "Fabric",
        icon: "hero-share",
        items: [item(:links, "Links", "hero-link", facts.links)]
      },
      %{
        label: "Routing",
        icon: "hero-arrows-right-left",
        items: [
          item(:paths, "Segment lists", "hero-queue-list", facts.paths, facts.paths_degraded),
          item(:computed, "Computed paths", "hero-cpu-chip", facts.computed),
          item(:registry, "SID allocations", "hero-hashtag", nil)
        ]
      },
      %{
        label: "Tenants",
        icon: "hero-building-office-2",
        items: [item(:sites, "Sites", "hero-map-pin", facts.sites, facts.sites - facts.sites_up)]
      },
      %{
        label: "Operations",
        icon: "hero-bolt",
        items: [item(:changes, "Changes", "hero-arrow-path", facts.changes)]
      }
    ]
  end

  # a section's badge is its size, unless something in it is wrong: then it is
  # how many things are wrong, in the bad tone
  defp item(key, label, icon, count, wrong \\ 0) do
    %{
      key: key,
      label: label,
      icon: icon,
      badge: if(wrong > 0, do: wrong, else: count),
      alert: wrong > 0
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
        <.nav groups={@nav} section={@section} />
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

      <div class="space-y-4" phx-window-keyup="clear" phx-key="Escape">
        <h1 class="sr-only">Pane</h1>
        <.summary facts={@facts} issues={@issues} />

        <div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
          <div class="min-w-0 space-y-4">
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

            <section class="mt-card overflow-hidden">
              <.card_head
                title={section_title(@section)}
                caption={section_caption(@section, @facts, @registry)}
              >
                <:aside :if={section_meta(@section, @facts)}>
                  <span class="font-mono text-xs text-muted">{section_meta(@section, @facts)}</span>
                </:aside>
              </.card_head>

              <div class="mt-scroll overflow-x-auto">
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

          <aside id="inspector" class="mt-card overflow-hidden xl:sticky xl:top-16">
            <.panel
              selected={@selected}
              issues={@issues}
              computed={@computed}
              paths={@paths}
              links={@links}
              nodes={@graph.nodes}
            />
          </aside>
        </div>
      </div>
    </Layouts.app>
    """
  end
end
