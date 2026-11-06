defmodule MaetoPaneWeb.Pane.LinksTable do
  @moduledoc """
  Adjacencies with one line per PCE edge and direction: delay, reservation and
  state, expanding into every cost dimension the PCE sees on each direction.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  alias MaetoPane.Pane.Costs

  attr :links, :list, required: true
  attr :selection, :any, required: true
  attr :expanded, :any, required: true
  attr :classes, :list, default: []
  attr :model, :map, default: nil

  def links_table(assigns) do
    assigns = assign(assigns, :model, assigns.model || Costs.default_model())

    ~H"""
    <table class="w-full min-w-[640px] border-collapse text-left text-[13px]">
      <thead class="sticky top-0 z-10 bg-surface text-xs font-medium text-muted shadow-[inset_0_-1px_0_var(--mt-line)]">
        <tr>
          <th class="w-8 py-2.5"></th>
          <th class="py-2.5 pr-4">Adjacency</th>
          <th class="py-2.5">Role</th>
          <th class="py-2.5">PCE edges</th>
          <th class="py-2.5 text-right">Delay</th>
          <th class="py-2.5 text-right">Reserved</th>
          <th class="py-2.5 text-right">IGP</th>
          <th class="py-2.5 text-right">TE</th>
          <th class="px-4 py-2.5 text-right">State</th>
        </tr>
      </thead>
      <tbody
        :for={link <- Enum.sort_by(@links, &{&1.up, &1.id})}
        id={"link-#{:erlang.phash2(link.id)}"}
      >
        <tr
          class={[
            "cursor-pointer border-b border-line hover:bg-sunken/70",
            selected?(@selection, :link, link.id) && "bg-accent-soft"
          ]}
          phx-click="select"
          phx-value-kind="link"
          phx-value-id={link.id}
        >
          <td
            class="py-2.5 pl-3 text-faint hover:text-ink"
            phx-click="toggle_link"
            phx-value-id={link.id}
            title="Cost dimensions"
          >
            <.icon
              name="hero-chevron-right-mini"
              class={[
                "size-4 transition-transform",
                MapSet.member?(@expanded, link.id) && "rotate-90"
              ]}
            />
          </td>
          <td class="py-2.5 pr-4 font-mono font-medium whitespace-nowrap text-accent">
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
              {if edge.delay_ms, do: "#{num(edge.delay_ms)} ms", else: "–"}
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
        <tr :if={MapSet.member?(@expanded, link.id)} class="border-b border-line bg-sunken/30">
          <td></td>
          <td colspan="8" class="py-3 pr-4">
            <.breakdown link={link} classes={@classes} model={@model} />
          </td>
        </tr>
      </tbody>
      <tbody :if={@links == []}>
        <tr>
          <td colspan="9" class="px-4 py-10 text-center text-muted">
            No adjacencies
          </td>
        </tr>
      </tbody>
    </table>
    """
  end

  attr :link, :map, required: true
  attr :classes, :list, required: true
  attr :model, :map, required: true

  defp breakdown(assigns) do
    ~H"""
    <div class="flex flex-wrap gap-x-14 gap-y-5">
      <div :for={{arrow, edge} <- @link.directions} class="min-w-[300px]">
        <div class="mb-1.5 font-mono text-xs text-muted">
          {arrow} {edge.from} &rarr; {edge.to}
          <span class="text-faint">· {edge.kind} ×{length(edge.members)}</span>
        </div>
        <table :if={edge.costs != []} class="text-xs">
          <tr :for={dim <- edge.costs}>
            <td class="py-1 pr-6 whitespace-nowrap text-muted">{dim.label}</td>
            <td class="py-1 pr-5 text-right font-mono tabular-nums">{value(dim.value)}</td>
            <td class="py-1 pr-2 font-mono whitespace-nowrap text-faint tabular-nums">
              {dim.reads || reads_booked(dim.key, edge)}
            </td>
          </tr>
        </table>
        <p :if={edge.costs == []} class="text-xs text-faint">No measurement yet</p>
        <table :if={edge.costs != [] and @classes != []} class="mt-3 border-t border-line text-xs">
          <tr :for={class <- @classes}>
            <td class="pt-2 pr-6 text-muted">
              <.class_tag color={class.slot} name={class.name} />
            </td>
            <td class="pt-2 pr-5 text-right font-mono font-medium tabular-nums">
              {value(Costs.class_total(edge.raw, class.objective, @model))}
            </td>
          </tr>
        </table>
      </div>
    </div>
    """
  end

  defp value(v) when is_number(v), do: :erlang.float_to_binary(v * 1.0, decimals: 2)
  defp value(_v), do: "–"

  defp reads_booked("STATIC_UTILIZATION", edge),
    do: "#{num(edge.booked)}/#{num(edge.bookable)} Mbps"

  defp reads_booked(_key, _edge), do: nil
end
