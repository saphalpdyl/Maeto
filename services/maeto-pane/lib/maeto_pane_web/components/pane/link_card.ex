defmodule MaetoPaneWeb.Pane.LinkCard do
  @moduledoc """
  The floating card over the topology for a selected adjacency: every directed
  PCE edge with the cost dimensions the PCE weighs it by. Jitter and loss are
  still seeded by the controller and utilisation is what is reserved, not what
  flows, so the card says so rather than passing them off as measurements.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :link, :map, required: true

  def link_card(assigns) do
    ~H"""
    <div
      id="link-card"
      class="absolute top-3 right-3 z-10 w-[420px] max-w-[calc(100%-1.5rem)] rounded-lg border border-line bg-surface shadow-lg"
    >
      <header class="flex items-start justify-between gap-2 border-b border-line px-4 py-3">
        <div class="min-w-0">
          <h3 class="font-mono text-sm font-semibold">{@link.a} &harr; {@link.b}</h3>
          <p class="mt-0.5 text-[11px] text-muted">
            {members_up(@link)}/{length(@link.edges)} member link(s) up · {@link.role || "link"}
          </p>
        </div>
        <div class="flex items-center gap-2">
          <.pill tone={if @link.up, do: "ok", else: "bad"}>
            {if @link.up, do: "Up", else: "Down"}
          </.pill>
          <button
            id="link-card-close"
            phx-click="clear"
            class="rounded p-0.5 text-muted transition-colors hover:bg-sunken hover:text-ink"
            aria-label="Close"
          >
            <.icon name="hero-x-mark" class="size-4" />
          </button>
        </div>
      </header>

      <div :for={{label, edges} <- directions(@link)} class="border-b border-line last:border-0">
        <div class="bg-sunken/60 px-4 py-1.5 font-mono text-[11px] font-semibold">{label}</div>
        <table class="w-full text-left text-xs">
          <thead class="text-[10px] text-faint">
            <tr>
              <th class="px-4 py-1 font-medium">Member</th>
              <th class="py-1 text-right font-medium">Latency</th>
              <th class="py-1 text-right font-medium">Jitter*</th>
              <th class="py-1 text-right font-medium">Loss*</th>
              <th class="px-4 py-1 text-right font-medium">Booked</th>
            </tr>
          </thead>
          <tbody>
            <tr :for={edge <- edges} class="border-t border-line/60">
              <td class="px-4 py-1.5 font-mono text-muted">{member_label(edge)}</td>
              <td class="py-1.5 text-right font-mono tabular-nums">{ms(edge.cost)}</td>
              <td class="py-1.5 text-right font-mono tabular-nums text-muted">{ms(edge.jitter)}</td>
              <td class="py-1.5 text-right font-mono tabular-nums text-muted">{num(edge.loss)}</td>
              <td class="px-4 py-1.5 text-right font-mono tabular-nums whitespace-nowrap">
                <span class={["mr-1.5", load_text(load_tone(edge.ratio))]}>{pct(edge.ratio)}</span>
                <span class="text-faint">{num(edge.booked)}/{num(edge.bookable)}</span>
              </td>
            </tr>
            <tr :if={edges == []}>
              <td colspan="5" class="px-4 py-2 text-faint">No PCE edge in this direction</td>
            </tr>
          </tbody>
        </table>
      </div>

      <p class="px-4 py-2 text-[10px] text-faint">* seeded</p>
    </div>
    """
  end

  defp directions(link) do
    [
      {"#{link.a} → #{link.b}", link.forward},
      {"#{link.b} → #{link.a}", link.reverse}
    ]
  end

  defp members_up(link), do: Enum.count(link.edges, & &1["up"])

  # a member id reads "A:eth2-B:eth2"; the sending side's interface names it
  defp member_label(%{members: [member]}) do
    member |> String.split("-") |> List.first() |> String.split(":") |> List.last()
  end

  defp member_label(%{members: members}), do: "bundle ×#{length(members)}"

  defp load_text("bad"), do: "text-bad"
  defp load_text("warn"), do: "text-warn"
  defp load_text(_tone), do: "text-muted"

  defp ms(nil), do: "–"
  defp ms(value), do: "#{num(value)} ms"
end
