defmodule MaetoPaneWeb.Pane.Inspector.LinkPanel do
  @moduledoc """
  A selected adjacency: paths that cross it and every member link.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.UI

  attr :selected, :map, required: true
  attr :computed, :list, required: true

  def link_panel(assigns) do
    ~H"""
    <.head
      title={"#{@selected.a} ↔ #{@selected.b}"}
      subtitle={"#{length(@selected.edges)} member link(s)"}
      tone={if @selected.up, do: "ok", else: "bad"}
      state={if @selected.up, do: "Up", else: "Down"}
    />

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
        <.row label="Delay" value={"#{edge["delay_ms"]} ms"} />
        <.row label="State" value={if edge["up"], do: "up", else: "down"} />
      </dl>
    </.group>
    """
  end

  # every computed path that walks this adjacency, in either direction

  # every computed path that walks this adjacency, in either direction
  defp crossing(computed, link) do
    pair = Enum.sort([link.a, link.b])

    Enum.filter(computed, fn path ->
      path.nodes
      |> Enum.chunk_every(2, 1, :discard)
      |> Enum.any?(&(Enum.sort(&1) == pair))
    end)
  end
end
