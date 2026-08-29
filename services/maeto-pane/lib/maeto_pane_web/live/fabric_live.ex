defmodule MaetoPaneWeb.FabricLive do
  @moduledoc "Topology graph with a detail panel for the current selection."

  use MaetoPaneWeb, :live_view

  alias MaetoPane.Fabric
  alias MaetoPane.Fabric.Analysis
  alias MaetoPane.Fabric.Layout

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket), do: Fabric.subscribe()

    {:ok, socket |> assign(:selection, nil) |> load()}
  end

  @impl true
  def handle_info(:fabric_updated, socket), do: {:noreply, load(socket)}

  @impl true
  def handle_event("select", %{"kind" => "node", "id" => id}, socket) do
    {:noreply, socket |> assign(:selection, {:node, id}) |> resolve()}
  end

  def handle_event("select", %{"kind" => "link", "id" => id}, socket) do
    {:noreply, socket |> assign(:selection, {:link, id}) |> resolve()}
  end

  def handle_event("clear", _params, socket) do
    {:noreply, socket |> assign(:selection, nil) |> resolve()}
  end

  defp load(socket) do
    snapshot = Fabric.snapshot()
    nodes = Analysis.nodes(snapshot)

    socket
    |> assign(:connected_to_nats, snapshot.connected)
    |> assign(:has_control, not is_nil(snapshot.control))
    |> assign(:issues, Analysis.issues(nodes))
    |> assign(:graph, Analysis.graph(snapshot))
    |> resolve()
  end

  defp resolve(socket) do
    assign(socket, :selected, Analysis.select(socket.assigns.graph, socket.assigns.selection))
  end

  defp selected?(nil, _kind, _id), do: false
  defp selected?({kind, id}, kind, id), do: true
  defp selected?(_selection, _kind, _id), do: false

  defp stroke(:ok), do: "#059669"
  defp stroke(:converging), do: "#d97706"
  defp stroke(:issue), do: "#e11d48"
  defp stroke(:blind), do: "#7c3aed"
  defp stroke(:silent), do: "#94a3b8"

  @impl true
  def render(assigns) do
    ~H"""
    <Layouts.app flash={@flash}>
      <div class="mx-auto max-w-[1400px] space-y-4 py-4">
        <header class="flex flex-wrap items-baseline gap-3">
          <h1 class="text-2xl font-semibold">Fabric</h1>
          <span class={[
            "rounded px-2 py-1 text-xs font-medium",
            @connected_to_nats && "bg-emerald-100 text-emerald-800",
            !@connected_to_nats && "bg-amber-100 text-amber-800"
          ]}>
            {if @connected_to_nats, do: "watching kv", else: "connecting to nats"}
          </span>
          <span :if={!@has_control} class="rounded bg-base-200 px-2 py-1 text-xs">
            no control snapshot yet
          </span>
          <span :if={@has_control} class="rounded bg-base-200 px-2 py-1 font-mono text-xs">
            {length(@graph.nodes)} pops &middot; {Enum.sum(Enum.map(@graph.links, &length(&1.arcs)))} links &middot; snapshot {@graph.published_at ||
              "?"}
          </span>
          <span :if={@issues != []} class="rounded bg-rose-100 px-2 py-1 text-xs text-rose-800">
            {length(@issues)} issue(s)
          </span>
        </header>

        <div class="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_380px]">
          <div class="rounded border border-base-300 bg-base-100 p-2">
            <svg
              viewBox={"0 0 #{Layout.width()} #{Layout.height()}"}
              class="h-[520px] w-full"
              phx-click="clear"
            >
              <g :for={link <- @graph.links}>
                <path
                  :for={arc <- link.arcs}
                  d={arc.d}
                  fill="none"
                  stroke={if selected?(@selection, :link, link.id), do: "#2563eb", else: "#cbd5e1"}
                  stroke-width={if selected?(@selection, :link, link.id), do: 4, else: 2}
                  stroke-linecap="round"
                  stroke-dasharray={if arc.up, do: "none", else: "6 4"}
                  class="cursor-pointer"
                  phx-click="select"
                  phx-value-kind="link"
                  phx-value-id={link.id}
                />
                <text
                  :if={length(link.arcs) > 1}
                  x={(link.x1 + link.x2) / 2}
                  y={(link.y1 + link.y2) / 2 - 6}
                  text-anchor="middle"
                  class="pointer-events-none fill-base-content/40 text-[9px]"
                >
                  ×{length(link.arcs)}
                </text>
              </g>

              <g :for={node <- @graph.nodes} class="cursor-pointer">
                <circle
                  cx={node.x}
                  cy={node.y}
                  r={if selected?(@selection, :node, node.id), do: 22, else: 18}
                  fill="#ffffff"
                  stroke={stroke(node.status)}
                  stroke-width={if selected?(@selection, :node, node.id), do: 5, else: 3}
                  phx-click="select"
                  phx-value-kind="node"
                  phx-value-id={node.id}
                />
                <text
                  x={node.x}
                  y={node.y + 4}
                  text-anchor="middle"
                  class="pointer-events-none fill-base-content text-[11px] font-semibold"
                >
                  {node.id}
                </text>
                <text
                  x={node.x}
                  y={node.y + 34}
                  text-anchor="middle"
                  class="pointer-events-none fill-base-content/60 text-[10px]"
                >
                  {node.name}
                </text>
              </g>

              <text
                :if={@graph.nodes == []}
                x={Layout.width() / 2}
                y={Layout.height() / 2}
                text-anchor="middle"
                class="fill-base-content/50 text-sm"
              >
                waiting for a control snapshot
              </text>
            </svg>
          </div>

          <aside class="rounded border border-base-300 bg-base-100 p-4">
            <.panel selected={@selected} issues={@issues} />
          </aside>
        </div>
      </div>
    </Layouts.app>
    """
  end

  attr :selected, :any, required: true
  attr :issues, :list, required: true

  defp panel(%{selected: nil} = assigns) do
    ~H"""
    <h2 class="text-sm font-semibold uppercase tracking-wide text-base-content/60">Issues</h2>
    <p :if={@issues == []} class="mt-3 text-sm text-base-content/60">
      Nothing selected. Click a node or a link.
    </p>
    <ul class="mt-3 space-y-2">
      <li :for={issue <- @issues} class="rounded bg-rose-50 px-3 py-2 text-xs">
        <div class="font-mono font-semibold text-rose-800">{issue.kind}</div>
        <div class="font-mono">{issue.sid}</div>
        <div class="text-rose-700">{issue.detail}</div>
      </li>
    </ul>
    """
  end

  defp panel(%{selected: %{kind: :node}} = assigns) do
    ~H"""
    <div class="space-y-4">
      <div>
        <h2 class="text-lg font-semibold">{@selected.name}</h2>
        <p class="font-mono text-xs text-base-content/60">{@selected.id}</p>
      </div>

      <dl class="space-y-1 text-xs">
        <.row label="locator" value={@selected.locator} />
        <.row label="loopback" value={@selected.loopback} />
        <.row label="isis net" value={@selected.inventory && @selected.inventory["isis_net"]} />
        <.row
          label="reconcile"
          value={
            cond do
              is_nil(@selected.view) or not @selected.view.reporting? -> "no state reported"
              not @selected.view.observed -> "reported, but could not read the dataplane"
              @selected.view.converged -> "converged in #{@selected.view.passes} pass(es)"
              true -> "not converged"
            end
          }
        />
        <.row label="generation" value={@selected.view && @selected.view.generation} />
        <.row label="reported at" value={@selected.view && @selected.view.reported_at} />
      </dl>

      <p
        :if={@selected.view && @selected.view.error}
        class="rounded bg-rose-50 px-2 py-1 font-mono text-xs text-rose-800"
      >
        {@selected.view.error}
      </p>

      <section :if={@selected.faults != []}>
        <h3 class="text-xs font-semibold uppercase tracking-wide text-rose-700">Issues</h3>
        <ul class="mt-1 space-y-1">
          <li :for={fault <- @selected.faults} class="rounded bg-rose-50 px-2 py-1 text-xs">
            <span class="font-mono font-semibold text-rose-800">{fault.kind}</span>
            <span class="font-mono">{fault.sid}</span>
            <div class="text-rose-700">{fault.detail}</div>
          </li>
        </ul>
      </section>

      <section :if={@selected.view && @selected.view.tenants != []}>
        <h3 class="text-xs font-semibold uppercase tracking-wide text-base-content/60">Tenants</h3>
        <table class="mt-1 w-full text-left text-xs">
          <tbody class="divide-y divide-base-200">
            <tr :for={tenant <- @selected.view.tenants}>
              <td class="py-1 font-semibold">{tenant.id}</td>
              <td class="font-mono">{tenant.table_id || "-"}</td>
              <td class="font-mono">{tenant.dt46_sid}</td>
              <td class="text-right text-base-content/60">{tenant.portals} site(s)</td>
            </tr>
          </tbody>
        </table>
      </section>

      <section :if={@selected.view && @selected.view.sids != []}>
        <h3 class="text-xs font-semibold uppercase tracking-wide text-base-content/60">SIDs</h3>
        <table class="mt-1 w-full text-left text-xs">
          <tbody class="divide-y divide-base-200">
            <tr :for={sid <- @selected.view.sids}>
              <td class="py-1 font-mono">{sid.sid}</td>
              <td class="font-mono">{sid.observed_table || "-"}</td>
              <td class={[
                "text-right",
                sid.status == :ok && "text-emerald-700",
                sid.status != :ok && "text-rose-700"
              ]}>
                {sid.status}
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section :if={@selected.inventory && @selected.inventory["interfaces"] != []}>
        <h3 class="text-xs font-semibold uppercase tracking-wide text-base-content/60">
          Interfaces
        </h3>
        <table class="mt-1 w-full text-left text-xs">
          <tbody class="divide-y divide-base-200">
            <tr :for={iface <- @selected.inventory["interfaces"] || []}>
              <td class="py-1 font-mono">{iface["Name"]}</td>
              <td>{iface["Role"]}</td>
              <td>{iface["Peer"]}</td>
              <td class="font-mono">{iface["Address"]}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
    """
  end

  defp panel(%{selected: %{kind: :link}} = assigns) do
    ~H"""
    <div class="space-y-4">
      <div>
        <h2 class="text-lg font-semibold">{@selected.a} &harr; {@selected.b}</h2>
        <p class="text-xs text-base-content/60">
          {length(@selected.edges)} parallel link(s) &middot; {if @selected.up, do: "up", else: "down"}
        </p>
      </div>

      <div :for={edge <- @selected.edges} class="rounded border border-base-200 p-2 text-xs">
        <div class="font-mono font-semibold">{edge["id"]}</div>
        <dl class="mt-1 space-y-1">
          <.row label="subnet" value={edge["subnet"]} />
          <.row label={"#{edge["local"]} iface"} value={edge["local_iface"]} />
          <.row label={"#{edge["local"]} addr"} value={edge["local_addr"]} />
          <.row label={"#{edge["remote"]} iface"} value={edge["remote_iface"]} />
          <.row label={"#{edge["remote"]} addr"} value={edge["remote_addr"]} />
          <.row label="role" value={edge["role"]} />
          <.row label="metric" value={edge["metric"]} />
          <.row label="te metric" value={edge["te_metric"]} />
          <.row label="delay" value={"#{edge["delay_ms"]} ms"} />
          <.row label="state" value={if edge["up"], do: "up", else: "down"} />
        </dl>
      </div>
    </div>
    """
  end

  attr :label, :string, required: true
  attr :value, :any, default: nil

  defp row(assigns) do
    ~H"""
    <div class="flex justify-between gap-3">
      <dt class="text-base-content/60">{@label}</dt>
      <dd class="truncate font-mono">{@value || "-"}</dd>
    </div>
    """
  end
end
