defmodule MaetoPaneWeb.Pane.Topology do
  @moduledoc """
  The topology workspace: the PoP mesh drawn by the Topology hook (AntV X6), an
  overlay switcher for health, cost and the selected path's trace, and a
  floating card with the selected adjacency's directional costs.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :graph, :map, required: true
  attr :links, :list, required: true
  attr :sites_by_node, :map, required: true
  attr :overlay, :atom, required: true
  attr :overlays, :list, required: true
  attr :trace, :map, required: true
  attr :selection, :any, required: true
  attr :ceiling, :any, required: true

  def topology(assigns) do
    ~H"""
    <section class="border-b border-line px-6 py-5">
      <div class="mb-3 flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <h2 class="text-[15px] font-medium">Topology</h2>
        <p :if={overlay_caption(assigns)} class="text-[13px] text-faint">
          {overlay_caption(assigns)}
        </p>

        <div class="ml-auto flex gap-0.5 rounded-[7px] border border-line bg-sunken p-0.5">
          <button
            :for={{key, label} <- @overlays}
            id={"overlay-#{key}"}
            phx-click="overlay"
            phx-value-overlay={key}
            class={[
              "rounded-[5px] px-2.5 py-[3px] text-xs transition-colors",
              if(@overlay == key,
                do: "bg-surface text-ink shadow-sm",
                else: "text-muted hover:text-ink"
              )
            ]}
          >
            {label}
          </button>
        </div>
      </div>

      <div class="h-[440px] w-full overflow-hidden rounded-lg border border-line">
        <div
          id="topology"
          phx-hook="Topology"
          phx-update="ignore"
          data-graph={graph_payload(assigns)}
          class="size-full"
        >
        </div>
      </div>

      <footer class="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 pt-3 text-xs text-muted">
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
      </footer>
    </section>
    """
  end

  defp graph_payload(assigns) do
    Jason.encode!(%{
      nodes: Enum.map(assigns.graph.nodes, &node_payload(&1, assigns.sites_by_node)),
      links: Enum.map(assigns.links, &link_payload/1),
      overlay: assigns.overlay,
      trace: assigns.trace.nodes,
      selection: selection_payload(assigns.selection),
      ceiling: assigns.ceiling
    })
  end

  # a PE terminates customer sites; everything else only forwards
  defp node_payload(node, sites_by_node) do
    sites = Map.get(sites_by_node, node.id, 0)
    tenants = length((node.view && node.view.tenants) || [])

    %{
      id: node.id,
      name: node.name,
      status: node.status,
      role: if(sites > 0 or tenants > 0, do: "PE", else: "PR"),
      sites: sites,
      vpns: tenants,
      ifs: length((node.inventory && node.inventory["interfaces"]) || [])
    }
  end

  defp link_payload(link) do
    %{
      id: link.id,
      a: link.a,
      b: link.b,
      members: Enum.map(link.edges, &%{id: &1["id"], up: &1["up"] == true}),
      latency: link.latency
    }
  end

  defp selection_payload({kind, id}), do: %{kind: kind, id: id}
  defp selection_payload(_selection), do: nil

  defp overlay_caption(%{overlay: :cost} = assigns), do: "worst #{num(assigns.ceiling)} ms"
  defp overlay_caption(_assigns), do: nil
end
