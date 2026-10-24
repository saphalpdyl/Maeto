defmodule MaetoPaneWeb.Pane.ChangesTable do
  @moduledoc """
  Reroutes and gated near-misses, newest first, with the edge whose cost moved.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :changes, :list, required: true
  attr :selection, :any, required: true

  def changes_table(assigns) do
    ~H"""
    <table class="w-full min-w-[640px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">When</th>
          <th class="py-2.5">Pair</th>
          <th class="py-2.5">Change</th>
          <th class="py-2.5">Because</th>
          <th class="px-4 py-2.5 text-right">Cost</th>
        </tr>
      </thead>
      <tbody>
        <tr
          :for={{change, i} <- Enum.with_index(@changes)}
          id={"change-#{i}"}
          class="border-b border-line last:border-0"
        >
          <td class="px-4 py-2.5 font-mono text-xs whitespace-nowrap text-muted">
            {clock(change.at)}
          </td>
          <td class="py-2.5 font-mono font-medium whitespace-nowrap text-accent">
            {change.src} &rarr; {change.dst}
          </td>
          <td class="py-2.5 font-mono text-xs text-muted">
            <.pill tone={kind_tone(change.kind)}>{change.kind}</.pill>
            <span :if={change.from_nodes != []} class="ml-2">
              {Enum.join(change.from_nodes, " ")} &rarr;
            </span>
            <span class="text-ink">{Enum.join(change.to_nodes, " ")}</span>
          </td>
          <td class="py-2.5 text-xs text-muted">
            <%= case change.causes do %>
              <% [] -> %>
                <span class="text-faint">-</span>
              <% [lead | rest] -> %>
                <span class="font-mono text-ink">{lead.edge}</span>
                {num(lead.was)} &rarr; {num(lead.now)}
                <span class="text-faint">({pct(lead.share)})</span>
                <span :if={rest != []} class="text-faint">+{length(rest)} more</span>
            <% end %>
          </td>
          <td class="px-4 py-2.5 text-right font-mono tabular-nums whitespace-nowrap">
            <span :if={change.from_cost} class="text-muted">{num(change.from_cost)} &rarr;</span>
            {num(change.to_cost)}
          </td>
        </tr>
        <tr :if={@changes == []}>
          <td class="px-4 py-6 text-center text-xs text-faint" colspan="5">
            No path changes recorded yet
          </td>
        </tr>
      </tbody>
    </table>
    """
  end
end
