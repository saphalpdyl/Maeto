defmodule MaetoPaneWeb.Pane.LinksTable do
  @moduledoc """
  Adjacencies with one line per PCE edge and direction: delay, reservation and state.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :links, :list, required: true
  attr :selection, :any, required: true

  def links_table(assigns) do
    ~H"""
    <table class="w-full min-w-[640px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">Adjacency</th>
          <th class="py-2.5">Role</th>
          <th class="py-2.5">PCE edges</th>
          <th class="py-2.5 text-right">Delay</th>
          <th class="py-2.5 text-right">Reserved</th>
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
            <div :for={{arrow, edge} <- link.directions} title={Enum.join(edge.members, "\n")}>
              {arrow} {edge.kind} ×{length(edge.members)}
            </div>
            <span :if={link.down > 0} class="text-bad">{link.down} down</span>
          </td>
          <td class="py-2.5 text-right font-mono tabular-nums">
            <div :for={{arrow, edge} <- link.directions}>
              <span class="text-muted">{arrow}</span>
              {if edge.cost, do: "#{num(edge.cost)} ms", else: "–"}
            </div>
          </td>
          <td class="py-2.5 text-right font-mono text-xs tabular-nums whitespace-nowrap">
            <div class="flex items-center justify-end gap-2">
              <.pill tone={load_tone(link.peak)}>{round(link.peak * 100)}%</.pill>
              <div class="text-muted">
                <div :for={{arrow, edge} <- link.directions}>
                  {arrow} {num(edge.booked)}/{num(edge.bookable)}
                </div>
              </div>
            </div>
          </td>
          <td class="py-2.5 text-right font-mono tabular-nums text-muted">{link.metric}</td>
          <td class="py-2.5 text-right font-mono tabular-nums text-muted">{link.te_metric}</td>
          <td class="px-4 py-2.5 text-right">
            <.pill tone={if link.up, do: "ok", else: "bad"}>
              {if link.up, do: "Up", else: "Down"}
            </.pill>
          </td>
        </tr>
        <tr :if={@links == []}>
          <td colspan="8" class="px-4 py-10 text-center text-muted">
            No adjacencies
          </td>
        </tr>
      </tbody>
    </table>
    """
  end
end
