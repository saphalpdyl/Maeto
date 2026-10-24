defmodule MaetoPaneWeb.Pane.Inspector.NodePanel do
  @moduledoc """
  A selected PoP: SRv6 identity, reconciliation state, tenants, SIDs and interfaces.
  """

  use MaetoPaneWeb, :html

  alias MaetoPane.Pane.Paths

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :selected, :map, required: true
  attr :paths, :list, default: []
  attr :links, :list, default: []
  attr :nodes, :list, default: []

  def node_panel(assigns) do
    assigns =
      assigns
      |> assign(:roles, roles(assigns.selected.id, assigns.paths))
      |> assign(:adjacencies, adjacencies(assigns.selected.id, assigns.links, assigns.nodes))

    ~H"""
    <header class="flex items-start gap-2.5 border-b border-line px-4 py-3">
      <div class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-sunken font-semibold">
        {@selected.id}
      </div>
      <div class="min-w-0 flex-1">
        <h2 class="truncate text-sm font-semibold">{@selected.name || @selected.id}</h2>
        <p class="truncate font-mono text-[11px] text-muted">{@selected.locator || "no locator"}</p>
      </div>
      <.esc />
    </header>

    <dl class="grid grid-cols-2 gap-3 border-b border-line px-4 py-3.5">
      <div class="space-y-1">
        <dt class="text-xs text-faint">State</dt>
        <dd>
          <.pill tone={node_tone(@selected.status)}>{state_label(@selected.status)}</.pill>
        </dd>
      </div>
      <div class="space-y-1">
        <dt class="text-xs text-faint">Sites · tenants</dt>
        <dd class="font-mono">{sites(@selected)} · {tenants(@selected)}</dd>
      </div>
      <div class="space-y-1">
        <dt class="text-xs text-faint">Originates</dt>
        <dd class="font-mono">{@roles.originates} lists</dd>
      </div>
      <div class="space-y-1">
        <dt class="text-xs text-faint">Transit for</dt>
        <dd class="font-mono">{@roles.transit} lists</dd>
      </div>
    </dl>

    <section :if={@adjacencies != []} class="border-b border-line px-4 py-3">
      <h3 class="mb-1.5 text-[13px] font-medium">Adjacencies</h3>
      <button
        :for={adj <- @adjacencies}
        phx-click="select"
        phx-value-kind="link"
        phx-value-id={adj.link}
        class="-mx-2 flex w-[calc(100%+1rem)] items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-sunken/70"
      >
        <span class={["size-1.5 shrink-0 rounded-full", if(adj.up, do: "bg-ok", else: "bg-bad")]} />
        <span class="text-[13px]">{adj.name}</span>
        <span class="truncate font-mono text-[11px] text-faint">{adj.sid}</span>
        <span class="ml-auto rounded bg-sunken px-1.5 font-mono text-xs tabular-nums">
          {num(adj.cost)}
        </span>
      </button>
    </section>

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

  defp state_label(:ok), do: "Reconciled"
  defp state_label(:converging), do: "Converging"
  defp state_label(:issue), do: "Issue"
  defp state_label(:blind), do: "Blind"
  defp state_label(:silent), do: "Silent"

  defp tenants(%{view: %{tenants: tenants}}), do: length(tenants)
  defp tenants(_node), do: 0

  defp sites(%{view: %{tenants: tenants}}), do: Enum.sum_by(tenants, &(&1.portals || 0))
  defp sites(_node), do: 0

  # a pop originates the lists it is the ingress PE for and is transit for any
  # list whose hops cross it before the egress
  defp roles(id, paths) do
    %{
      originates: Enum.count(paths, &(&1.pe == id)),
      transit:
        Enum.count(paths, fn row ->
          chain = Paths.chain(row)
          row.pe != id and id in Enum.drop(chain, -1)
        end)
    }
  end

  # the End.X is the one this pop owns towards the neighbour; cost is the worst
  # probed latency leaving it in that direction
  defp adjacencies(id, links, nodes) do
    names = Map.new(nodes, &{&1.id, &1.name || &1.id})

    for link <- links, id in [link.a, link.b] do
      {peer, outbound} =
        if link.a == id, do: {link.b, link.forward}, else: {link.a, link.reverse}

      sid =
        Enum.find_value(link.edges, fn edge -> edge["local"] == id && edge["end_x"] end)

      %{
        link: link.id,
        name: Map.get(names, peer, peer),
        sid: sid,
        up: link.up,
        cost: outbound |> Enum.map(& &1.cost) |> Enum.reject(&is_nil/1) |> Enum.max(fn -> nil end)
      }
    end
  end
end
