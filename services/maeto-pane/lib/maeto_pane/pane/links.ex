defmodule MaetoPane.Pane.Links do
  @moduledoc """
  Shapes the graph's adjacencies for the links table: one entry per node pair,
  with every PCE edge in each direction and its cost and reservation.
  """

  def from_graph(graph) do
    pce = Enum.group_by(graph.pce_edges, &{&1["from"], &1["to"]}, &pce_edge_view/1)

    Enum.map(graph.links, fn link ->
      forward = Map.get(pce, {link.a, link.b}, [])
      reverse = Map.get(pce, {link.b, link.a}, [])

      Map.merge(link, %{
        directions: Enum.map(forward, &{"→", &1}) ++ Enum.map(reverse, &{"←", &1}),
        peak: (forward ++ reverse) |> Enum.map(& &1.ratio) |> Enum.max(fn -> 0 end),
        cost: (forward ++ reverse) |> Enum.map(&(&1.cost || 0)) |> Enum.max(fn -> 0 end),
        role: link.edges |> List.first() |> then(&(&1 && &1["role"])),
        metric: link.edges |> Enum.map(&(&1["metric"] || 0)) |> Enum.max(fn -> 0 end),
        te_metric: link.edges |> Enum.map(&(&1["te_metric"] || 0)) |> Enum.max(fn -> 0 end),
        down: Enum.count(link.edges, &(not &1["up"])),
        forward: forward,
        reverse: reverse,
        latency: worst_latency(forward ++ reverse)
      })
    end)
  end

  # A PCE edge is what the PCE routes and books on: directed, either a bundle of
  # parallel members or a single member. Cost is the worst member's latency, and
  # stays nil until a probe has landed, which is not the same as zero.
  defp pce_edge_view(edge) do
    state = edge["reservation"] || %{}
    booked = state["booked_mbps"] || 0
    bookable = state["bookable_mbps"] || 0

    %{
      id: edge["id"],
      kind: edge["kind"],
      members: edge["members"] || [],
      from: edge["from"],
      to: edge["to"],
      cost: get_in(edge, ["costs", "LATENCY"]),
      jitter: get_in(edge, ["costs", "JITTER"]),
      loss: get_in(edge, ["costs", "LOSS"]),
      capacity: edge["capacity_mbps"],
      booked: booked,
      bookable: bookable,
      ratio: utilisation(booked, bookable)
    }
  end

  # nil until a probe has landed on any direction, so the graph can say "no
  # measurement" instead of drawing a zero
  defp worst_latency(edges) do
    case Enum.reject(Enum.map(edges, & &1.cost), &is_nil/1) do
      [] -> nil
      costs -> Enum.max(costs)
    end
  end

  defp utilisation(_booked, bookable) when bookable <= 0, do: 0
  defp utilisation(booked, bookable), do: booked / bookable
end
