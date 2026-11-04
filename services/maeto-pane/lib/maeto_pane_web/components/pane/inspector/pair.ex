defmodule MaetoPaneWeb.Pane.Inspector.PairPanel do
  @moduledoc """
  A selected tenant site pair: the path the PCE computed for each class, every
  one drawn in its class color on the topology.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.UI

  attr :selected, :map, required: true

  def pair_panel(assigns) do
    ~H"""
    <header class="flex items-start gap-2.5 border-b border-line px-4 py-3">
      <div class="min-w-0 flex-1">
        <h2 class="truncate text-sm font-semibold">
          {@selected.source} &rarr; {@selected.dest}
        </h2>
        <p class="mt-0.5 truncate text-xs text-muted">
          {subtitle(@selected)}
        </p>
      </div>
      <.esc />
    </header>

    <section
      :for={path <- @selected.paths}
      class={[
        "space-y-2 border-b border-line px-4 py-3.5",
        (@selected.focus != nil and @selected.focus != path.label) && "opacity-50"
      ]}
    >
      <div class="flex items-center justify-between gap-2">
        <.class_tag color={path.slot} name={path.class} class="text-[13px] font-medium" />
        <span class="font-mono text-xs text-muted tabular-nums">cost {num(path.cost)}</span>
      </div>
      <ol class="flex flex-wrap items-center gap-1 font-mono text-xs">
        <li :for={{node, index} <- Enum.with_index(path.nodes)} class="flex items-center gap-1">
          <span :if={index > 0} class="text-faint">›</span>
          <span
            class="rounded border px-1.5 py-px"
            style={"border-color: #{class_color(path.slot)}"}
          >
            {node}
          </span>
        </li>
      </ol>
      <p class="text-xs text-faint">{plural(path.hops, "hop", "hops")}</p>
    </section>
    """
  end

  defp subtitle(%{tenant: nil, paths: paths}), do: plural(length(paths), "class", "classes")

  defp subtitle(%{tenant: tenant, paths: paths}),
    do: "tenant #{tenant} · #{plural(length(paths), "class", "classes")}"

  defp plural(1, one, _many), do: "1 #{one}"
  defp plural(count, _one, many), do: "#{count} #{many}"
end
