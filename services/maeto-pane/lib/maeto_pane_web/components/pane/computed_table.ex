defmodule MaetoPaneWeb.Pane.ComputedTable do
  @moduledoc """
  The PCE's computed path set, one row per tenant pair and class.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.UI

  attr :computed, :list, required: true
  attr :programmed, :any, required: true

  def computed_table(assigns) do
    ~H"""
    <table class="w-full min-w-[600px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">Pair</th>
          <th class="py-2.5">Underlay hops</th>
          <th class="py-2.5">Class</th>
          <th class="py-2.5 text-right">Cost</th>
          <th class="px-4 py-2.5 text-right">Programmed</th>
        </tr>
      </thead>
      <tbody>
        <tr :for={path <- @computed} class="border-b border-line last:border-0 hover:bg-sunken/70">
          <td class="px-4 py-2 font-mono font-medium whitespace-nowrap">
            {path.source} &rarr; {path.dest}
            <span :if={path.tenant} class="ml-1.5 text-xs text-muted">tenant {path.tenant}</span>
          </td>
          <td class="py-2 font-mono text-xs text-muted">{Enum.join(path.nodes, " › ")}</td>
          <td class="py-2 text-muted">
            {path.class}
            <span :if={path.label} class="ml-1 font-mono text-xs text-faint">{path.label}</span>
          </td>
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
            No path set
          </td>
        </tr>
      </tbody>
    </table>
    """
  end
end
