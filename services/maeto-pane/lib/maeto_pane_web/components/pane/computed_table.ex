defmodule MaetoPaneWeb.Pane.ComputedTable do
  @moduledoc """
  The PCE's computed path set, one row per tenant site pair, expanding into the
  path each class takes between them.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.UI

  attr :pairs, :list, required: true
  attr :programmed, :any, required: true
  attr :expanded, :any, required: true
  attr :selection, :any, required: true

  def computed_table(assigns) do
    ~H"""
    <table class="w-full min-w-[600px] border-collapse text-left text-[13px]">
      <thead class="sticky top-0 z-10 bg-surface text-xs font-medium text-muted shadow-[inset_0_-1px_0_var(--mt-line)]">
        <tr>
          <th class="w-8 py-2.5"></th>
          <th class="py-2.5 pr-4">Pair</th>
          <th class="py-2.5 pr-4">Classes</th>
          <th class="py-2.5">Underlay hops</th>
          <th class="py-2.5 text-right">Cost</th>
          <th class="px-4 py-2.5 text-right">Programmed</th>
        </tr>
      </thead>
      <tbody :for={pair <- @pairs} id={"pair-#{:erlang.phash2(pair.key)}"}>
        <tr
          class={[
            "cursor-pointer border-b border-line transition-colors hover:bg-sunken/70",
            selected?(@selection, :pair, pair.key) &&
              "bg-steer-soft shadow-[inset_2px_0_0_var(--mt-steer)]"
          ]}
          phx-click="select"
          phx-value-kind="pair"
          phx-value-id={pair.key}
        >
          <td
            class="py-2 pl-3 text-faint hover:text-ink"
            phx-click="toggle_pair"
            phx-value-id={pair.key}
          >
            <.icon
              name="hero-chevron-right-mini"
              class={[
                "size-4 transition-transform",
                MapSet.member?(@expanded, pair.key) && "rotate-90"
              ]}
            />
          </td>
          <td class="py-2 pr-4 whitespace-nowrap">
            <span class="font-mono font-medium">{pair.source} &rarr; {pair.dest}</span>
            <span :if={pair.tenant} class="ml-1.5 text-xs text-muted">tenant {pair.tenant}</span>
          </td>
          <td class="py-2 pr-4">
            <span class="flex items-center gap-1">
              <span
                :for={path <- pair.paths}
                class="size-2 rounded-full"
                style={"background: #{class_color(path.slot)}"}
                title={path.class}
              />
              <span class="ml-1 text-xs text-muted">{length(pair.paths)}</span>
            </span>
          </td>
          <td class="py-2 font-mono text-xs text-muted">{routes(pair)}</td>
          <td class="py-2 text-right font-mono tabular-nums">{cost_range(pair)}</td>
          <td class="px-4 py-2 text-right">
            <.programmed programmed={@programmed} source={pair.source} dest={pair.dest} />
          </td>
        </tr>
        <tr
          :for={path <- pair.paths}
          :if={MapSet.member?(@expanded, pair.key)}
          class={[
            "cursor-pointer border-b border-line/60 bg-sunken/30 transition-colors hover:bg-sunken/70",
            selected?(@selection, :computed, computed_key(path)) &&
              "bg-steer-soft shadow-[inset_2px_0_0_var(--mt-steer)]"
          ]}
          phx-click="select"
          phx-value-kind="computed"
          phx-value-id={computed_key(path)}
        >
          <td></td>
          <td class="py-2 pr-4 pl-4">
            <.class_tag color={path.slot} name={path.class} />
          </td>
          <td class="py-2 pr-4 font-mono text-xs text-faint">{path.label}</td>
          <td class="py-2 font-mono text-xs text-muted">{Enum.join(path.nodes, " › ")}</td>
          <td class="py-2 text-right font-mono tabular-nums">{num(path.cost)}</td>
          <td></td>
        </tr>
      </tbody>
      <tbody :if={@pairs == []}>
        <tr>
          <td colspan="6" class="px-4 py-10 text-center text-muted">
            No path set
          </td>
        </tr>
      </tbody>
    </table>
    """
  end

  attr :programmed, :any, required: true
  attr :source, :string, required: true
  attr :dest, :string, required: true

  defp programmed(assigns) do
    ~H"""
    <.pill :if={MapSet.member?(@programmed, {@source, @dest})} tone="ok">Segment list</.pill>
    <span :if={!MapSet.member?(@programmed, {@source, @dest})} class="text-xs text-faint">
      No tenant pair
    </span>
    """
  end

  def computed_key(path), do: "#{path.tenant}|#{path.source}|#{path.dest}|#{path.label}"

  defp routes(%{paths: [path]}), do: Enum.join(path.nodes, " › ")

  defp routes(%{paths: paths}) do
    case paths |> Enum.map(& &1.nodes) |> Enum.uniq() do
      [nodes] -> Enum.join(nodes, " › ")
      distinct -> "#{length(distinct)} distinct routes"
    end
  end

  defp cost_range(%{paths: paths}) do
    costs = paths |> Enum.map(& &1.cost) |> Enum.reject(&is_nil/1)

    case Enum.uniq(costs) do
      [] -> "-"
      [cost] -> num(cost)
      _ -> "#{num(Enum.min(costs))}–#{num(Enum.max(costs))}"
    end
  end
end
