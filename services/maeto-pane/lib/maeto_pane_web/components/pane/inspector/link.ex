defmodule MaetoPaneWeb.Pane.Inspector.LinkPanel do
  @moduledoc """
  A selected adjacency: probed latency, jitter and loss per direction, what is
  booked on it, the paths that cross it and every member link.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.LineChart
  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :selected, :map, required: true
  attr :computed, :list, required: true
  attr :links, :list, default: []
  attr :stats, :map, default: nil

  def link_panel(assigns) do
    assigns = assign(assigns, :view, Enum.find(assigns.links, &(&1.id == assigns.selected.id)))

    ~H"""
    <.head
      title={"#{@selected.a} ↔ #{@selected.b}"}
      subtitle={"#{Enum.count(@selected.edges, & &1["up"])}/#{length(@selected.edges)} member link(s) up"}
      tone={if @selected.up, do: "ok", else: "bad"}
      state={if @selected.up, do: "Up", else: "Down"}
    />

    <section class="space-y-5 border-b border-line px-4 py-4">
      <div class="flex items-center gap-4 text-xs text-muted">
        <span class="flex items-center gap-1.5">
          <span class="h-0.5 w-3.5 rounded-full bg-ok" />{@selected.a} &rarr; {@selected.b}
        </span>
        <span class="flex items-center gap-1.5">
          <span class="flex w-3.5 justify-between">
            <span class="size-[3px] rounded-full bg-steer" />
            <span class="size-[3px] rounded-full bg-steer" />
            <span class="size-[3px] rounded-full bg-steer" />
          </span>
          {@selected.b} &rarr; {@selected.a}
        </span>
      </div>

      <p :if={@stats == nil} class="text-[13px] text-muted">No probe history</p>

      <%= if @stats do %>
        <.line_chart
          id="chart-latency"
          title="Latency"
          unit="ms"
          solid={@stats.forward.latency}
          dotted={@stats.reverse.latency}
          from={@stats.from}
        />
        <.line_chart
          id="chart-jitter"
          title="Jitter"
          unit="ms"
          solid={@stats.forward.jitter}
          dotted={@stats.reverse.jitter}
          from={@stats.from}
        />
        <.line_chart
          id="chart-loss"
          title="Loss"
          unit="%"
          solid={@stats.forward.loss}
          dotted={@stats.reverse.loss}
          from={@stats.from}
          floor={5.0}
        />
      <% end %>
    </section>

    <.group :if={@view} title="Booked">
      <div :for={{label, edges} <- directions(@view)} class="mb-2 last:mb-0">
        <div class="mb-1 font-mono text-[11px] text-muted">{label}</div>
        <div :for={edge <- edges} class="flex items-center gap-2 py-0.5 text-xs">
          <span class="font-mono text-faint">{member_label(edge)}</span>
          <span class={["ml-auto font-mono tabular-nums", load_text(load_tone(edge.ratio))]}>
            {pct(edge.ratio)}
          </span>
          <span class="w-[86px] text-right font-mono text-faint tabular-nums">
            {num(edge.booked)}/{num(edge.bookable)}
          </span>
        </div>
        <div :if={edges == []} class="text-xs text-faint">No PCE edge</div>
      </div>
    </.group>

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
        None
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
        <.row label="State" value={if edge["up"], do: "up", else: "down"} />
      </dl>
    </.group>
    """
  end

  defp directions(view) do
    [
      {"#{view.a} → #{view.b}", view.forward},
      {"#{view.b} → #{view.a}", view.reverse}
    ]
  end

  defp member_label(%{members: [member]}) do
    member |> String.split("-") |> List.first() |> String.split(":") |> List.last()
  end

  defp member_label(%{members: members}), do: "bundle ×#{length(members)}"

  defp load_text("bad"), do: "text-bad-ink"
  defp load_text("warn"), do: "text-warn-ink"
  defp load_text(_tone), do: "text-muted"

  defp crossing(computed, link) do
    pair = Enum.sort([link.a, link.b])

    Enum.filter(computed, fn path ->
      path.nodes
      |> Enum.chunk_every(2, 1, :discard)
      |> Enum.any?(&(Enum.sort(&1) == pair))
    end)
  end
end
