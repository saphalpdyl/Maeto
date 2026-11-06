defmodule MaetoPane.Pane.Links do
  @moduledoc """
  Shapes the graph's adjacencies for the links table: one entry per node pair,
  with every PCE edge in each direction and its cost and reservation.
  """

  alias MaetoPane.Pane.Costs

  def from_graph(graph, model \\ Costs.default_model()) do
    pce = Enum.group_by(graph.pce_edges, &{&1["from"], &1["to"]}, &pce_edge_view(&1, model))

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
  # parallel members or a single member. Cost is the worst member's normalized
  # latency cost and delay_ms the same read back in ms; both stay nil until a
  # probe has landed. costs keeps every dimension as the controller published it.
  @doc """
  Prices every link for one class the way CSPF does, worst direction first, so
  the map can label and shade links by what that class actually pays.
  """
  def put_class_cost(links, class, model) do
    objective = (class && class.objective) || %{}

    Enum.map(links, fn link ->
      totals =
        (link.forward ++ link.reverse)
        |> Enum.map(&Costs.class_total(&1.raw, objective, model))
        |> Enum.reject(&is_nil/1)

      Map.put(link, :class_cost, if(totals == [], do: nil, else: Enum.max(totals)))
    end)
  end

  defp pce_edge_view(edge, model) do
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
      delay_ms: Costs.delay_ms(get_in(edge, ["costs", "LATENCY"]), model),
      costs: Costs.breakdown(edge["costs"], model),
      raw: edge["costs"],
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
