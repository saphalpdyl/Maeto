defmodule MaetoPaneWeb.FabricLive do
  @moduledoc """
  The fabric console.

  Table-first, with the topology as a persistent orientation strip and a side
  inspector. Follows the causal chain in Maeto.design.md -- link condition ->
  cost -> computed path -> segment list -> programmed dataplane -> observed
  state -- in the visual language of the Aether console.
  """

  use MaetoPaneWeb, :live_view

  alias MaetoPane.Fabric
  alias MaetoPane.Fabric.Analysis
  alias MaetoPane.Fabric.Paths

  @sections [
    {:links, "Links", "hero-link"},
    {:computed, "Computed", "hero-cpu-chip"},
    {:paths, "Segment lists", "hero-arrows-right-left"},
    {:sites, "Sites", "hero-map-pin"},
    {:registry, "Registry", "hero-server-stack"}
  ]

  @overlays [health: "Health", cost: "Cost", path: "Path"]

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket), do: Fabric.subscribe()

    {:ok,
     socket
     |> assign(:selection, nil)
     |> assign(:section, :paths)
     |> assign(:overlay, :health)
     |> assign(:sections, @sections)
     |> assign(:overlays, @overlays)
     |> assign(:page_title, "Fabric")
     |> load()}
  end

  @impl true
  def handle_info(:fabric_updated, socket), do: {:noreply, load(socket)}

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
      when section in ~w(paths links computed sites registry) do
    {:noreply, assign(socket, :section, String.to_existing_atom(section))}
  end

  def handle_event("overlay", %{"overlay" => overlay}, socket)
      when overlay in ~w(health cost path) do
    {:noreply, assign(socket, :overlay, String.to_existing_atom(overlay))}
  end

  defp load(socket) do
    snapshot = Fabric.snapshot()

    nodes = Analysis.nodes(snapshot)
    registry = Analysis.registry(snapshot)
    graph = Analysis.graph(snapshot)
    rows = Paths.rows(snapshot)
    sites = Paths.sites(snapshot)
    links = links(graph)
    computed = Paths.computed(snapshot)

    socket
    |> assign(:connected_to_nats, snapshot.connected)
    |> assign(:has_control, not is_nil(snapshot.control))
    |> assign(:graph, graph)
    |> assign(:registry, registry)
    |> assign(:paths, rows)
    |> assign(:sites, sites)
    |> assign(:links, links)
    |> assign(:computed, computed)
    |> assign(:sites_by_node, Enum.frequencies_by(sites, & &1.attach))
    |> assign(:issues, Analysis.issues(nodes) ++ Analysis.drift(registry) ++ Paths.issues(rows))
    |> assign(:facts, facts(graph, links, sites, rows, computed))
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

  # One row per physical link. The control snapshot carries a single edge record
  # per interface pair, so a per-direction cost is not available here yet.
  defp links(graph) do
    Enum.map(graph.links, fn link ->
      Map.merge(link, %{
        cost: link.edges |> Enum.map(&(&1["delay_ms"] || 0)) |> Enum.min(fn -> 0 end),
        role: link.edges |> List.first() |> then(&(&1 && &1["role"])),
        metric: link.edges |> Enum.map(&(&1["metric"] || 0)) |> Enum.max(fn -> 0 end),
        te_metric: link.edges |> Enum.map(&(&1["te_metric"] || 0)) |> Enum.max(fn -> 0 end),
        down: Enum.count(link.edges, &(not &1["up"]))
      })
    end)
  end

  defp facts(graph, links, sites, rows, computed) do
    members = Enum.flat_map(graph.links, & &1.edges)
    site_counts = Paths.site_counts(sites)
    path_counts = Paths.path_counts(rows)

    %{
      pops: length(graph.nodes),
      adjacencies: length(members),
      adjacencies_up: Enum.count(members, & &1["up"]),
      links: length(links),
      sites: site_counts.total,
      sites_up: site_counts.up,
      paths: path_counts.total,
      paths_installed: path_counts.installed,
      paths_degraded: path_counts.degraded,
      computed: length(computed)
    }
  end

  defp count(:paths, facts), do: facts.paths
  defp count(:links, facts), do: facts.links
  defp count(:computed, facts), do: facts.computed
  defp count(:sites, facts), do: facts.sites
  defp count(:registry, _facts), do: nil

  defp selected?(nil, _kind, _id), do: false
  defp selected?({kind, id}, kind, id), do: true
  defp selected?(_selection, _kind, _id), do: false

  defp node_tone(:ok), do: "ok"
  defp node_tone(:converging), do: "warn"
  defp node_tone(:issue), do: "bad"
  defp node_tone(:blind), do: "idle"
  defp node_tone(:silent), do: "faint"

  defp dot("ok"), do: "bg-ok"
  defp dot("warn"), do: "bg-warn"
  defp dot("bad"), do: "bg-bad"
  defp dot("idle"), do: "bg-idle"
  defp dot("accent"), do: "bg-accent"
  defp dot(_tone), do: "bg-faint"

  defp pill_classes("ok"), do: "border-ok/20 bg-ok-soft text-ok"
  defp pill_classes("warn"), do: "border-warn/20 bg-warn-soft text-warn"
  defp pill_classes("bad"), do: "border-bad/20 bg-bad-soft text-bad"
  defp pill_classes("idle"), do: "border-idle/20 bg-idle-soft text-idle"
  defp pill_classes("accent"), do: "border-accent/20 bg-accent-soft text-accent"
  defp pill_classes(_tone), do: "border-line bg-sunken text-muted"

  defp path_tone(:installed), do: "ok"
  defp path_tone(:drifted), do: "bad"
  defp path_tone(:missing), do: "bad"
  defp path_tone(:orphan), do: "warn"
  defp path_tone(:pending), do: "warn"
  defp path_tone(_status), do: "faint"

  defp site_tone(:up), do: "ok"
  defp site_tone(:converging), do: "warn"
  defp site_tone(:no_tunnel), do: "warn"
  defp site_tone(:no_report), do: "idle"
  defp site_tone(:offline), do: "faint"

  defp path_label(:installed), do: "Reconciled"
  defp path_label(:drifted), do: "Drift"
  defp path_label(:missing), do: "Not in FIB"
  defp path_label(:orphan), do: "Orphan"
  defp path_label(:pending), do: "Pending"
  defp path_label(status), do: to_string(status)

  defp site_label(:up), do: "Online"
  defp site_label(:converging), do: "Converging"
  defp site_label(:no_tunnel), do: "No tunnel"
  defp site_label(:no_report), do: "No report"
  defp site_label(:offline), do: "Offline"

  defp reconcile_note(:installed), do: "the FIB matches the intent"
  defp reconcile_note(:drifted), do: "the kernel holds a different segment list"
  defp reconcile_note(:missing), do: "rendered by the node, absent from the kernel"
  defp reconcile_note(:orphan), do: "in the kernel, but no intent asks for it"
  defp reconcile_note(:pending), do: "the node has not rendered this intent yet"
  defp reconcile_note(_status), do: ""

  defp num(nil), do: "-"
  defp num(value) when is_float(value), do: :erlang.float_to_binary(value, decimals: 1)
  defp num(value), do: to_string(value)

  defp clock(nil), do: "-"

  defp clock(stamp) when is_binary(stamp) do
    case DateTime.from_iso8601(stamp) do
      {:ok, at, _offset} -> Calendar.strftime(at, "%H:%M:%S")
      _ -> stamp
    end
  end

  defp section_title(:paths), do: "Segment lists"
  defp section_title(:computed), do: "Computed paths"
  defp section_title(:links), do: "Links"
  defp section_title(:sites), do: "Sites"
  defp section_title(:registry), do: "Service registry"

  defp section_caption(:paths, facts, _registry),
    do: "#{facts.paths_installed} of #{facts.paths} programmed, desired against the observed FIB"

  defp section_caption(:computed, facts, _registry),
    do: "Least cost per dimension, recomputed on every PCE tick (#{facts.computed} pairs)"

  defp section_caption(:links, _facts, _registry),
    do: "Cost is published per link, not yet per direction"

  defp section_caption(:sites, facts, _registry),
    do: "#{facts.sites_up} of #{facts.sites} portals reporting in"

  defp section_caption(:registry, _facts, registry) do
    if registry.present?,
      do: "SID cursor #{registry.cursor}, #{length(registry.allocated)} allocated",
      else: "The controller has not published its registry yet"
  end

  # every computed path that walks this adjacency, in either direction
  defp crossing(computed, link) do
    pair = Enum.sort([link.a, link.b])

    Enum.filter(computed, fn path ->
      path.nodes
      |> Enum.chunk_every(2, 1, :discard)
      |> Enum.any?(&(Enum.sort(&1) == pair))
    end)
  end

  @impl true
  def render(assigns) do
    ~H"""
    <Layouts.app flash={@flash}>
      <:rail>
        <button
          :for={{key, label, icon} <- @sections}
          id={"nav-#{key}"}
          phx-click="section"
          phx-value-section={key}
          class={[
            "flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-[13px] transition-colors",
            if(@section == key,
              do: "bg-sunken text-ink",
              else: "text-muted hover:bg-sunken/60 hover:text-ink"
            )
          ]}
        >
          <.icon name={icon} class="size-4 shrink-0" />
          <span class="truncate">{label}</span>
          <span
            :if={key == :paths and @facts.paths_degraded > 0}
            class="ml-auto font-mono text-[10px] text-bad"
          >
            {@facts.paths_degraded}
          </span>
        </button>
      </:rail>

      <:crumbs>
        <span class="shrink-0 text-[13px] text-muted">Control plane</span>
        <span class="shrink-0 text-faint">/</span>
        <span class="shrink-0 text-[13px]">Fabric</span>
        <span
          :if={@graph.domain["locator_prefix"]}
          class="ml-1 shrink-0 rounded border border-line px-1.5 py-0.5 font-mono text-[10px] text-muted"
        >
          {@graph.domain["locator_prefix"]}
        </span>
      </:crumbs>

      <:status>
        <span class={[
          "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium",
          pill_classes(if @connected_to_nats, do: "ok", else: "warn")
        ]}>
          <span class={[
            "size-1.5 rounded-full",
            dot(if @connected_to_nats, do: "ok", else: "warn")
          ]} />
          {if @connected_to_nats, do: "Live", else: "Reconnecting"}
        </span>
        <span class="hidden font-mono text-[11px] text-faint sm:inline">
          {if @has_control, do: clock(@graph.published_at), else: "no snapshot"}
        </span>
      </:status>

      <div class="space-y-6">
        <header>
          <h1 class="text-2xl font-semibold tracking-tight">Fabric</h1>
          <div class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px] text-muted">
            <span>{@facts.pops} PoP mesh</span>
            <span class="text-line-strong">·</span>
            <.tally
              value={"#{@facts.adjacencies_up}/#{@facts.adjacencies}"}
              label="adjacencies up"
              bad={@facts.adjacencies_up < @facts.adjacencies}
            />
            <span class="text-line-strong">·</span>
            <.tally
              value={"#{@facts.paths_installed}/#{@facts.paths}"}
              label="segment lists in FIB"
              bad={@facts.paths_degraded > 0}
            />
            <span class="text-line-strong">·</span>
            <.tally
              value={"#{@facts.sites_up}/#{@facts.sites}"}
              label="sites up"
              bad={@facts.sites > 0 and @facts.sites_up < @facts.sites}
            />
            <span class="text-line-strong">·</span>
            <.tally value={length(@issues)} label="issues" bad={@issues != []} />
          </div>
        </header>

        <section class="space-y-3">
          <div class="flex flex-wrap items-center gap-3">
            <div>
              <h2 class="text-[15px] font-medium">Topology</h2>
              <p class="mt-0.5 text-xs text-muted">{overlay_caption(assigns)}</p>
            </div>

            <div class="ml-auto flex items-center gap-0.5 rounded-md border border-line p-0.5">
              <button
                :for={{key, label} <- @overlays}
                id={"overlay-#{key}"}
                phx-click="overlay"
                phx-value-overlay={key}
                class={[
                  "rounded px-2.5 py-1 text-xs transition-colors",
                  if(@overlay == key,
                    do: "bg-sunken text-ink",
                    else: "text-muted hover:text-ink"
                  )
                ]}
              >
                {label}
              </button>
            </div>
          </div>

          <div class="rounded-lg border border-line bg-surface">
            <div
              id="topology"
              phx-hook="Topology"
              phx-update="ignore"
              data-graph={graph_payload(assigns)}
              class="h-[440px] w-full"
            >
            </div>

            <footer class="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-t border-line px-4 py-2 text-xs text-muted">
              <%= if @overlay == :health do %>
                <span
                  :for={
                    {status, label} <- [
                      ok: "Reconciled",
                      converging: "Converging",
                      issue: "Issue",
                      blind: "Blind",
                      silent: "Silent"
                    ]
                  }
                  class="flex items-center gap-1.5"
                >
                  <span class={["size-1.5 rounded-full", dot(node_tone(status))]} />{label}
                </span>
                <span class="flex items-center gap-1.5">
                  <span class="h-0.5 w-3.5 rounded-full bg-bad" />Down
                </span>
              <% end %>
              <span :if={@overlay == :path and @trace.nodes != []} class="font-mono text-accent">
                {Enum.join(@trace.nodes, " › ")}
              </span>
              <span :if={@overlay == :path and @trace.nodes == []}>
                Select a segment list to trace it across the mesh
              </span>
              <span class="ml-auto hidden items-center gap-1.5 sm:flex">
                <span class="size-1.5 rounded-full bg-accent" />Hosts sites
              </span>
              <span class="hidden text-faint lg:inline">scroll to zoom, drag to pan</span>
            </footer>
          </div>
        </section>

        <section class="space-y-3">
          <div>
            <h2 class="flex items-baseline gap-2 text-[15px] font-medium">
              {section_title(@section)}
              <span :if={count(@section, @facts)} class="font-mono text-xs text-faint">
                {count(@section, @facts)}
              </span>
            </h2>
            <p class="mt-0.5 text-xs text-muted">{section_caption(@section, @facts, @registry)}</p>
          </div>

          <div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
            <div class="mt-scroll overflow-x-auto rounded-lg border border-line bg-surface">
              <%= case @section do %>
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

            <aside
              id="inspector"
              class="rounded-lg border border-line bg-surface xl:sticky xl:top-16"
            >
              <.panel selected={@selected} issues={@issues} computed={@computed} />
            </aside>
          </div>
        </section>
      </div>
    </Layouts.app>
    """
  end

  defp graph_payload(assigns) do
    Jason.encode!(%{
      nodes:
        Enum.map(assigns.graph.nodes, fn node ->
          %{
            id: node.id,
            name: node.name,
            status: node.status,
            locator: node.locator,
            sites: Map.get(assigns.sites_by_node, node.id, 0),
            tenants: length((node.view && node.view.tenants) || [])
          }
        end),
      edges: assigns.graph.edges,
      overlay: assigns.overlay,
      trace: assigns.trace.nodes,
      selection: selection_payload(assigns.selection),
      ceiling: assigns.ceiling
    })
  end

  defp selection_payload({kind, id}), do: %{kind: kind, id: id}
  defp selection_payload(_selection), do: nil

  defp overlay_caption(%{overlay: :cost} = assigns),
    do: "Latency cost per link, thicker is costlier · ceiling #{num(assigns.ceiling)} ms"

  defp overlay_caption(%{overlay: :path}), do: "The selected segment list, traced"
  defp overlay_caption(_assigns), do: "Reconciliation state per PoP"

  attr :value, :any, required: true
  attr :label, :string, required: true
  attr :bad, :boolean, default: false

  defp tally(assigns) do
    ~H"""
    <span class="flex items-center gap-1.5 whitespace-nowrap">
      <span class={["font-mono tabular-nums", if(@bad, do: "text-bad", else: "text-ink")]}>
        {@value}
      </span>
      <span>{@label}</span>
    </span>
    """
  end

  attr :paths, :list, required: true
  attr :selection, :any, required: true

  defp paths_table(assigns) do
    ~H"""
    <table class="w-full min-w-[840px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">Towards</th>
          <th class="py-2.5">Tenant</th>
          <th class="py-2.5">Destination prefix</th>
          <th class="py-2.5">Segment list</th>
          <th class="py-2.5 text-right">Cost</th>
          <th class="px-4 py-2.5 text-right">Dataplane</th>
        </tr>
      </thead>
      <tbody :for={{pe, rows} <- Enum.sort(Enum.group_by(@paths, & &1.pe))}>
        <tr class="border-b border-line bg-sunken/60">
          <td colspan="6" class="px-4 py-1.5">
            <span class="font-mono text-xs font-semibold">{pe}</span>
            <span class="ml-2 text-xs text-muted">ingress · {length(rows)} list(s)</span>
          </td>
        </tr>
        <tr
          :for={row <- rows}
          id={"path-#{:erlang.phash2(row.id)}"}
          class={[
            "cursor-pointer border-b border-line hover:bg-sunken/70",
            selected?(@selection, :path, row.id) && "bg-accent-soft"
          ]}
          phx-click="select"
          phx-value-kind="path"
          phx-value-id={row.id}
        >
          <td class="px-4 py-2.5 whitespace-nowrap">
            <span class="font-medium text-accent">{row.dest_name || row.dest || "?"}</span>
            <span :if={row.site} class="ml-1.5 text-xs text-muted">{row.site.cpe}</span>
          </td>
          <td class="py-2.5 font-mono text-xs tabular-nums">{row.tenant}</td>
          <td class="py-2.5 font-mono text-xs text-muted">{row.prefix}</td>
          <td class="py-2.5"><.chain hops={row.hops} /></td>
          <td class="py-2.5 text-right font-mono tabular-nums">{num(row.cost)}</td>
          <td class="px-4 py-2.5 text-right">
            <.pill tone={path_tone(row.status)}>{path_label(row.status)}</.pill>
          </td>
        </tr>
      </tbody>
      <tbody :if={@paths == []}>
        <tr>
          <td colspan="6" class="px-4 py-10 text-center text-muted">
            No segment list programmed yet. The PCE publishes one per tenant site pair.
          </td>
        </tr>
      </tbody>
    </table>
    """
  end

  attr :links, :list, required: true
  attr :selection, :any, required: true

  defp links_table(assigns) do
    ~H"""
    <table class="w-full min-w-[640px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">Adjacency</th>
          <th class="py-2.5">Role</th>
          <th class="py-2.5">Members</th>
          <th class="py-2.5 text-right">Delay</th>
          <th class="py-2.5 text-right">IGP</th>
          <th class="py-2.5 text-right">TE</th>
          <th class="px-4 py-2.5 text-right">State</th>
        </tr>
      </thead>
      <tbody>
        <tr
          :for={link <- Enum.sort_by(@links, &{&1.up, &1.id})}
          id={"link-#{:erlang.phash2(link.id)}"}
          class={[
            "cursor-pointer border-b border-line last:border-0 hover:bg-sunken/70",
            selected?(@selection, :link, link.id) && "bg-accent-soft"
          ]}
          phx-click="select"
          phx-value-kind="link"
          phx-value-id={link.id}
        >
          <td class="px-4 py-2.5 font-mono font-medium whitespace-nowrap text-accent">
            {link.a} &harr; {link.b}
          </td>
          <td class="py-2.5 text-muted">{link.role}</td>
          <td class="py-2.5 font-mono text-xs tabular-nums text-muted">
            ×{length(link.edges)}
            <span :if={link.down > 0} class="ml-1 text-bad">{link.down} down</span>
          </td>
          <td class="py-2.5 text-right font-mono tabular-nums">{num(link.cost)} ms</td>
          <td class="py-2.5 text-right font-mono tabular-nums text-muted">{link.metric}</td>
          <td class="py-2.5 text-right font-mono tabular-nums text-muted">{link.te_metric}</td>
          <td class="px-4 py-2.5 text-right">
            <.pill tone={if link.up, do: "ok", else: "bad"}>
              {if link.up, do: "Up", else: "Down"}
            </.pill>
          </td>
        </tr>
        <tr :if={@links == []}>
          <td colspan="7" class="px-4 py-10 text-center text-muted">
            No adjacency in the control snapshot.
          </td>
        </tr>
      </tbody>
    </table>
    """
  end

  attr :computed, :list, required: true
  attr :programmed, :any, required: true

  defp computed_table(assigns) do
    ~H"""
    <table class="w-full min-w-[600px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">Pair</th>
          <th class="py-2.5">Underlay hops</th>
          <th class="py-2.5">Dimension</th>
          <th class="py-2.5 text-right">Cost</th>
          <th class="px-4 py-2.5 text-right">Programmed</th>
        </tr>
      </thead>
      <tbody>
        <tr :for={path <- @computed} class="border-b border-line last:border-0 hover:bg-sunken/70">
          <td class="px-4 py-2 font-mono font-medium whitespace-nowrap">
            {path.source} &rarr; {path.dest}
          </td>
          <td class="py-2 font-mono text-xs text-muted">{Enum.join(path.nodes, " › ")}</td>
          <td class="py-2 text-muted">{path.dimension}</td>
          <td class="py-2 text-right font-mono tabular-nums">{num(path.cost)}</td>
          <td class="px-4 py-2 text-right">
            <.pill :if={MapSet.member?(@programmed, {path.source, path.dest})} tone="ok">
              Segment list
            </.pill>
            <span
              :if={!MapSet.member?(@programmed, {path.source, path.dest})}
              class="text-xs text-faint"
            >
              No tenant pair
            </span>
          </td>
        </tr>
        <tr :if={@computed == []}>
          <td colspan="5" class="px-4 py-10 text-center text-muted">
            The PCE has not published a path set yet.
          </td>
        </tr>
      </tbody>
    </table>
    """
  end

  attr :sites, :list, required: true
  attr :selection, :any, required: true

  defp sites_table(assigns) do
    ~H"""
    <table class="w-full min-w-[720px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">Site</th>
          <th class="py-2.5">Tenant</th>
          <th class="py-2.5">Prefix</th>
          <th class="py-2.5">Attach</th>
          <th class="py-2.5">Portal</th>
          <th class="py-2.5 text-right">If</th>
          <th class="px-4 py-2.5 text-right">State</th>
        </tr>
      </thead>
      <tbody>
        <tr
          :for={site <- @sites}
          id={"site-#{site.portal_id}"}
          class={[
            "cursor-pointer border-b border-line last:border-0 hover:bg-sunken/70",
            selected?(@selection, :site, site.portal_id) && "bg-accent-soft"
          ]}
          phx-click="select"
          phx-value-kind="site"
          phx-value-id={site.portal_id}
        >
          <td class="px-4 py-2.5 font-medium text-accent">{site.cpe}</td>
          <td class="py-2.5 font-mono text-xs tabular-nums">{site.tenant}</td>
          <td class="py-2.5 font-mono text-xs text-muted">{site.prefix}</td>
          <td class="py-2.5 whitespace-nowrap">{site.attach_node}</td>
          <td class="py-2.5 font-mono text-xs text-faint">{site.portal_id}</td>
          <td class="py-2.5 text-right font-mono text-xs tabular-nums text-muted">
            {site.tunnel_if || site.if_id}
          </td>
          <td class="px-4 py-2.5 text-right">
            <.pill tone={site_tone(site.status)}>{site_label(site.status)}</.pill>
          </td>
        </tr>
        <tr :if={@sites == []}>
          <td colspan="7" class="px-4 py-10 text-center text-muted">
            No tenant database in the control snapshot.
          </td>
        </tr>
      </tbody>
    </table>
    """
  end

  attr :registry, :map, required: true

  defp registry_panel(assigns) do
    ~H"""
    <div :if={!@registry.present?} class="px-4 py-10 text-center text-[13px] text-muted">
      The controller has not published its registry yet.
    </div>

    <div :if={@registry.present?}>
      <table class="w-full min-w-[520px] border-collapse text-left text-[13px]">
        <thead class="border-b border-line text-xs font-medium text-muted">
          <tr>
            <th class="px-4 py-2.5">Tenant</th>
            <th class="py-2.5">Behaviour</th>
            <th class="py-2.5">Locator</th>
            <th class="px-4 py-2.5">SID</th>
          </tr>
        </thead>
        <tbody>
          <tr
            :for={allocation <- @registry.tenants}
            class="border-b border-line hover:bg-sunken/70"
          >
            <td class="px-4 py-2 font-mono font-medium tabular-nums">{allocation.tenant}</td>
            <td class="py-2 font-mono text-xs uppercase text-muted">{allocation.type}</td>
            <td class="py-2 font-mono text-xs text-muted">{allocation.locator}</td>
            <td class="px-4 py-2 font-mono text-xs">{allocation.sid}</td>
          </tr>
          <tr :if={@registry.tenants == []}>
            <td colspan="4" class="px-4 py-6 text-center text-muted">Nothing allocated</td>
          </tr>
        </tbody>
      </table>

      <div class="border-t border-line bg-sunken/60 px-4 py-2">
        <h3 class="text-xs font-semibold">Held intents</h3>
      </div>
      <table class="w-full min-w-[520px] border-collapse text-left text-[13px]">
        <thead class="border-b border-line text-xs font-medium text-muted">
          <tr>
            <th class="px-4 py-2.5">Node</th>
            <th class="py-2.5">Gen</th>
            <th class="py-2.5">Tenant</th>
            <th class="py-2.5">Sites</th>
            <th class="px-4 py-2.5">End.DT46</th>
          </tr>
        </thead>
        <tbody>
          <%= for node <- @registry.nodes, tenant <- node.tenants do %>
            <tr class={["border-b border-line last:border-0", tenant.drifted? && "bg-bad-soft"]}>
              <td class="px-4 py-2 font-mono font-medium">{node.id}</td>
              <td class="py-2 font-mono text-xs tabular-nums text-muted">{node.generation}</td>
              <td class="py-2 font-mono text-xs tabular-nums">{tenant.id}</td>
              <td class="py-2 tabular-nums text-muted">{tenant.portals}</td>
              <td class="px-4 py-2 font-mono text-xs">
                {tenant.registry_sid}
                <span :if={tenant.drifted?} class="text-bad">
                  (published {tenant.published_sid || "nothing"})
                </span>
              </td>
            </tr>
          <% end %>
          <tr :if={@registry.nodes == []}>
            <td colspan="5" class="px-4 py-6 text-center text-muted">No intents held</td>
          </tr>
        </tbody>
      </table>
    </div>
    """
  end

  attr :tone, :any, default: "faint"
  slot :inner_block, required: true

  defp pill(assigns) do
    ~H"""
    <span class={[
      "inline-flex shrink-0 items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium whitespace-nowrap",
      pill_classes(@tone)
    ]}>
      <span class={["size-1.5 rounded-full", dot(@tone)]} />
      {render_slot(@inner_block)}
    </span>
    """
  end

  attr :hops, :list, required: true

  defp chain(assigns) do
    ~H"""
    <div class="flex flex-wrap items-center gap-1">
      <span :if={@hops == []} class="text-xs text-faint">no segments</span>
      <span :for={{hop, index} <- Enum.with_index(@hops)} class="inline-flex items-center gap-1">
        <span :if={index > 0} class="text-faint">&rsaquo;</span>
        <span
          class={[
            "inline-flex items-center gap-1 rounded border px-1.5 py-0.5 font-mono text-[11px]",
            if(hop.role == :decap,
              do: "border-accent/25 bg-accent-soft text-accent",
              else: "border-line bg-sunken text-muted"
            )
          ]}
          title={hop.node_name || "no locator owns this segment"}
        >
          {hop.sid}
          <span :if={hop.role == :decap} class="text-[9px] uppercase opacity-70">dt46</span>
        </span>
      </span>
    </div>
    """
  end

  attr :selected, :any, required: true
  attr :issues, :list, required: true
  attr :computed, :list, required: true

  defp panel(%{selected: nil} = assigns) do
    ~H"""
    <.head title="Inspector" subtitle="Nothing selected" />

    <p :if={@issues == []} class="px-4 py-4 text-[13px] text-muted">
      No invariant violation. Select a PoP, an adjacency, a segment list or a site to inspect it
      without leaving the topology.
    </p>

    <div :if={@issues != []}>
      <div class="border-b border-line px-4 py-2">
        <h3 class="text-xs font-semibold text-bad">{length(@issues)} issue(s)</h3>
      </div>
      <ul class="mt-scroll max-h-[420px] overflow-auto">
        <li :for={issue <- @issues} class="border-b border-line px-4 py-2.5 last:border-0">
          <div class="flex items-baseline justify-between gap-2">
            <span class="font-mono text-xs font-semibold text-bad">{issue.kind}</span>
            <span :if={issue.node} class="font-mono text-[11px] text-faint">{issue.node}</span>
          </div>
          <div class="mt-0.5 font-mono text-xs">{issue.sid}</div>
          <div class="mt-1 text-xs text-muted">{issue.detail}</div>
        </li>
      </ul>
    </div>
    """
  end

  defp panel(%{selected: %{kind: :path}} = assigns) do
    ~H"""
    <.head
      title={"#{@selected.pe} → #{@selected.dest || "?"}"}
      subtitle={"Tenant #{@selected.tenant} · #{@selected.prefix}"}
      tone={path_tone(@selected.status)}
      state={path_label(@selected.status)}
    />

    <.group title="Decision">
      <.row label="Objective" value="least latency cost" />
      <.row label="Path cost" value={num(@selected.cost)} />
      <.row label="Underlay hops" value={length(Paths.chain(@selected)) - 1} />
      <.row label="Egress PoP" value={@selected.dest_name} />
      <.row label="Egress site" value={@selected.site && @selected.site.cpe} />
    </.group>

    <.group title="Segment list">
      <ol class="space-y-1">
        <li
          :for={{hop, index} <- Enum.with_index(@selected.hops, 1)}
          class="flex items-center gap-2 rounded border border-line bg-sunken/60 px-2 py-1.5"
        >
          <span class="w-3 font-mono text-[11px] text-faint">{index}</span>
          <span class="font-mono text-xs">{hop.sid}</span>
          <span class="ml-auto text-[11px] text-muted">{hop.node_name || "unresolved"}</span>
          <span class={[
            "font-mono text-[9px] uppercase",
            if(hop.role == :decap, do: "text-accent", else: "text-faint")
          ]}>
            {if hop.role == :decap, do: "end.dt46", else: "end"}
          </span>
        </li>
      </ol>
    </.group>

    <.group title="Desired against actual">
      <div class="space-y-2">
        <div>
          <div class="text-[11px] text-faint">Intent</div>
          <div class="font-mono text-xs">{Enum.join(@selected.segments, " › ")}</div>
        </div>
        <div>
          <div class="text-[11px] text-faint">Observed FIB</div>
          <div class={["font-mono text-xs", @selected.status == :drifted && "text-bad"]}>
            {if @selected.observed,
              do: Enum.join(@selected.observed, " › "),
              else: "no matching route"}
          </div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <.pill tone={path_tone(@selected.status)}>{path_label(@selected.status)}</.pill>
          <span class="text-xs text-muted">{reconcile_note(@selected.status)}</span>
        </div>
      </div>
    </.group>

    <.group title="VRF">
      <.row label="Table" value={@selected.table} />
      <.row label="Tenant" value={@selected.tenant} />
    </.group>
    """
  end

  defp panel(%{selected: %{portal_id: _portal}} = assigns) do
    ~H"""
    <.head
      title={@selected.cpe}
      subtitle={@selected.identity}
      tone={site_tone(@selected.status)}
      state={site_label(@selected.status)}
    />

    <.group title="Service">
      <.row label="Tenant" value={@selected.tenant} />
      <.row label="Allocation" value={@selected.allocation} />
      <.row label="Site prefix" value={@selected.prefix} />
      <.row label="VRF table" value={@selected.vrf_table} />
    </.group>

    <.group title="Attachment">
      <.row label="Attach PoP" value={@selected.attach} />
      <.row label="PoP name" value={@selected.attach_node} />
      <.row label="Portal id" value={@selected.portal_id} />
      <.row label="Provisioned if" value={@selected.if_id} />
      <.row label="Tunnel if" value={@selected.tunnel_if} />
      <.row label="Reported at" value={clock(@selected.reported_at)} />
    </.group>
    """
  end

  defp panel(%{selected: %{kind: :node}} = assigns) do
    ~H"""
    <.head
      title={@selected.name || @selected.id}
      subtitle={@selected.id}
      tone={node_tone(@selected.status)}
      state={to_string(@selected.status)}
    />

    <.group title="SRv6">
      <.row label="Locator" value={@selected.locator} />
      <.row label="Loopback" value={@selected.loopback} />
      <.row label="ISIS net" value={@selected.inventory && @selected.inventory["isis_net"]} />
    </.group>

    <.group title="Reconciliation">
      <.row
        label="State"
        value={
          cond do
            is_nil(@selected.view) or not @selected.view.reporting? -> "no state reported"
            not @selected.view.observed -> "could not read the dataplane"
            @selected.view.converged -> "converged in #{@selected.view.passes} pass(es)"
            true -> "not converged"
          end
        }
      />
      <.row label="Generation" value={@selected.view && @selected.view.generation} />
      <.row label="Reported at" value={@selected.view && clock(@selected.view.reported_at)} />
    </.group>

    <div :if={@selected.view && @selected.view.error} class="border-b border-line px-4 py-2.5">
      <p class="rounded border-l-2 border-bad bg-bad-soft px-2 py-1.5 font-mono text-xs text-bad">
        {@selected.view.error}
      </p>
    </div>

    <.group :if={@selected.faults != []} title="Issues">
      <ul class="space-y-1">
        <li
          :for={fault <- @selected.faults}
          class="rounded border-l-2 border-bad bg-bad-soft px-2 py-1.5 text-xs"
        >
          <span class="font-mono font-semibold text-bad">{fault.kind}</span>
          <span class="font-mono">{fault.sid}</span>
          <div class="mt-0.5 text-muted">{fault.detail}</div>
        </li>
      </ul>
    </.group>

    <.group :if={@selected.view && @selected.view.tenants != []} title="Tenants">
      <table class="w-full text-left text-xs">
        <tbody>
          <tr :for={tenant <- @selected.view.tenants} class="border-b border-line last:border-0">
            <td class="py-1.5 font-mono font-semibold tabular-nums">{tenant.id}</td>
            <td class="font-mono tabular-nums text-muted">{tenant.table_id || "-"}</td>
            <td class="font-mono">{tenant.dt46_sid}</td>
            <td class="text-right text-muted">{tenant.portals} site(s)</td>
          </tr>
        </tbody>
      </table>
    </.group>

    <.group :if={@selected.view && @selected.view.sids != []} title="SIDs in FIB">
      <table class="w-full text-left text-xs">
        <tbody>
          <tr :for={sid <- @selected.view.sids} class="border-b border-line last:border-0">
            <td class="py-1.5 font-mono">{sid.sid}</td>
            <td class="font-mono tabular-nums text-muted">{sid.observed_table || "-"}</td>
            <td class="py-1.5 text-right">
              <.pill tone={if sid.status == :ok, do: "ok", else: "bad"}>{sid.status}</.pill>
            </td>
          </tr>
        </tbody>
      </table>
    </.group>

    <.group :if={@selected.inventory && @selected.inventory["interfaces"] != []} title="Interfaces">
      <table class="w-full text-left text-xs">
        <tbody>
          <tr
            :for={iface <- @selected.inventory["interfaces"] || []}
            class="border-b border-line last:border-0"
          >
            <td class="py-1.5 font-mono">{iface["Name"]}</td>
            <td class="text-muted">{iface["Role"]}</td>
            <td>{iface["Peer"]}</td>
            <td class="font-mono text-muted">{iface["Address"]}</td>
          </tr>
        </tbody>
      </table>
    </.group>
    """
  end

  defp panel(%{selected: %{kind: :link}} = assigns) do
    ~H"""
    <.head
      title={"#{@selected.a} ↔ #{@selected.b}"}
      subtitle={"#{length(@selected.edges)} member link(s)"}
      tone={if @selected.up, do: "ok", else: "bad"}
      state={if @selected.up, do: "Up", else: "Down"}
    />

    <.group title="Paths across this adjacency">
      <ul
        :if={crossing(@computed, @selected) != []}
        class="mt-scroll max-h-40 space-y-1 overflow-auto"
      >
        <li :for={path <- crossing(@computed, @selected)} class="flex items-baseline gap-2 text-xs">
          <span class="font-mono font-semibold">{path.source} &rarr; {path.dest}</span>
          <span class="truncate font-mono text-faint">{Enum.join(path.nodes, "›")}</span>
          <span class="ml-auto font-mono tabular-nums text-muted">{num(path.cost)}</span>
        </li>
      </ul>
      <p :if={crossing(@computed, @selected) == []} class="text-xs text-muted">
        No computed path currently traverses this adjacency.
      </p>
    </.group>

    <.group :for={edge <- @selected.edges} title={edge["id"]}>
      <dl class="space-y-1">
        <.row label="Subnet" value={edge["subnet"]} />
        <.row label={"#{edge["local"]} iface"} value={edge["local_iface"]} />
        <.row label={"#{edge["local"]} addr"} value={edge["local_addr"]} />
        <.row label={"#{edge["remote"]} iface"} value={edge["remote_iface"]} />
        <.row label={"#{edge["remote"]} addr"} value={edge["remote_addr"]} />
        <.row label="Role" value={edge["role"]} />
        <.row label="IGP metric" value={edge["metric"]} />
        <.row label="TE metric" value={edge["te_metric"]} />
        <.row label="Delay" value={"#{edge["delay_ms"]} ms"} />
        <.row label="State" value={if edge["up"], do: "up", else: "down"} />
      </dl>
    </.group>
    """
  end

  attr :title, :string, required: true
  attr :subtitle, :string, default: nil
  attr :tone, :any, default: nil
  attr :state, :any, default: nil

  defp head(assigns) do
    ~H"""
    <header class="border-b border-line px-4 py-3">
      <div class="flex items-start justify-between gap-2">
        <h2 class="min-w-0 truncate text-sm font-semibold">{@title}</h2>
        <.pill :if={@state} tone={@tone || "faint"}>{@state}</.pill>
      </div>
      <p :if={@subtitle} class="mt-0.5 truncate font-mono text-[11px] text-faint">{@subtitle}</p>
    </header>
    """
  end

  attr :title, :string, required: true
  slot :inner_block, required: true

  defp group(assigns) do
    ~H"""
    <section class="border-b border-line px-4 py-3 last:border-0">
      <h3 class="mb-2 text-xs font-semibold text-muted">{@title}</h3>
      {render_slot(@inner_block)}
    </section>
    """
  end

  attr :label, :string, required: true
  attr :value, :any, default: nil

  defp row(assigns) do
    ~H"""
    <div class="flex justify-between gap-3 text-xs">
      <dt class="shrink-0 text-muted">{@label}</dt>
      <dd class="truncate font-mono">{@value || "-"}</dd>
    </div>
    """
  end
end
