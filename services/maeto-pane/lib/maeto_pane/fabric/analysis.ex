defmodule MaetoPane.Fabric.Analysis do
  @moduledoc "Derives per-node views and invariant violations from a fabric snapshot."

  alias MaetoPane.Fabric.Layout

  @arc_spacing 16
  @node_radius 22

  def nodes(%{intents: intents, states: states}) do
    (Map.keys(intents) ++ Map.keys(states))
    |> Enum.uniq()
    |> Enum.filter(&String.starts_with?(&1, "pop."))
    |> Enum.sort()
    |> Enum.map(&build(&1, Map.get(intents, &1), Map.get(states, &1)))
  end

  def issues(nodes), do: duplicate_sids(nodes) ++ row_issues(nodes)

  def graph(snapshot) do
    control = Map.get(snapshot, :control) || %{}
    raw_nodes = get_in(control, ["topology", "nodes"]) || []
    raw_edges = get_in(control, ["topology", "edges"]) || []
    inventory = Map.new(get_in(control, ["inventory"]) || [], &{&1["id"], &1})

    views = nodes(snapshot)
    by_id = Map.new(views, &{&1.id, &1})
    faults = views |> issues() |> Enum.group_by(& &1.node)

    links = group_links(raw_edges)
    positions = Layout.positions(Enum.map(raw_nodes, & &1["id"]), Enum.map(links, &{&1.a, &1.b}))

    %{
      nodes: Enum.map(raw_nodes, &graph_node(&1, positions, inventory, by_id, faults)),
      links: Enum.map(links, &place(&1, positions)),
      domain: get_in(control, ["topology", "domain"]) || %{},
      published_at: control["published_at"]
    }
  end

  def select(graph, {:node, id}), do: Enum.find(graph.nodes, &(&1.id == id))

  def select(graph, {:link, id}), do: Enum.find(graph.links, &(&1.id == id))

  def select(_graph, _selection), do: nil

  defp graph_node(raw, positions, inventory, views, faults) do
    id = raw["id"]
    {x, y} = Map.get(positions, id, {0.0, 0.0})
    view = Map.get(views, id)
    node_faults = Map.get(faults, id, [])

    %{
      kind: :node,
      id: id,
      name: raw["name"],
      locator: raw["locator"],
      loopback: raw["loopback"],
      x: x,
      y: y,
      view: view,
      inventory: Map.get(inventory, id),
      faults: node_faults,
      status: node_status(view, node_faults)
    }
  end

  defp node_status(nil, _faults), do: :silent

  defp node_status(%{reporting?: false}, _faults), do: :silent

  defp node_status(%{observed: false}, _faults), do: :blind

  defp node_status(_view, [_ | _]), do: :issue

  defp node_status(%{converged: true}, []), do: :ok

  defp node_status(_view, []), do: :converging

  defp group_links(edges) do
    edges
    |> Enum.group_by(fn edge -> Enum.sort([edge["local"], edge["remote"]]) end)
    |> Enum.map(fn {[a, b], grouped} ->
      %{
        kind: :link,
        id: "#{a}|#{b}",
        a: a,
        b: b,
        edges: Enum.sort_by(grouped, & &1["id"]),
        up: Enum.any?(grouped, & &1["up"])
      }
    end)
    |> Enum.sort_by(& &1.id)
  end

  defp place(link, positions) do
    {x1, y1} = Map.get(positions, link.a, {0.0, 0.0})
    {x2, y2} = Map.get(positions, link.b, {0.0, 0.0})
    count = length(link.edges)

    arcs =
      link.edges
      |> Enum.with_index()
      |> Enum.map(fn {edge, index} ->
        offset = (index - (count - 1) / 2) * @arc_spacing

        %{id: edge["id"], up: edge["up"], d: arc({x1, y1}, {x2, y2}, offset)}
      end)

    Map.merge(link, %{x1: x1, y1: y1, x2: x2, y2: y2, arcs: arcs})
  end

  defp arc({x1, y1}, {x2, y2}, offset) do
    dx = x2 - x1
    dy = y2 - y1
    span = max(:math.sqrt(dx * dx + dy * dy), 0.001)
    ux = dx / span
    uy = dy / span

    sx = x1 + ux * @node_radius
    sy = y1 + uy * @node_radius
    ex = x2 - ux * @node_radius
    ey = y2 - uy * @node_radius

    cx = (sx + ex) / 2 - uy * offset * 2
    cy = (sy + ey) / 2 + ux * offset * 2

    "M #{round1(sx)} #{round1(sy)} Q #{round1(cx)} #{round1(cy)} #{round1(ex)} #{round1(ey)}"
  end

  defp round1(value), do: Float.round(value * 1.0, 1)

  defp build(key, intent, state) do
    tenants = tenants(intent, state)

    %{
      id: String.replace_prefix(key, "pop.", ""),
      generation: intent && intent["generation"],
      reported_generation: state && state["generation"],
      converged: state && state["converged"],
      observed: is_nil(state) or Map.get(state, "observed", true),
      passes: state && state["passes"],
      reported_at: state && state["reported_at"],
      error: state && state["error"],
      reporting?: not is_nil(state),
      tenants: tenants,
      sids: sids(tenants, state)
    }
  end

  defp tenants(intent, state) do
    tables = vrf_tables(state)

    intent
    |> get_in_safe(["intent", "tenants"])
    |> Enum.map(fn {tenant_id, tenant} ->
      %{
        id: tenant_id,
        dt46_sid: tenant["dt46_sid"],
        portals: map_size(tenant["portals"] || %{}),
        table_id: Map.get(tables, tenant_id)
      }
    end)
    |> Enum.sort_by(& &1.id)
  end

  defp vrf_tables(state) do
    state
    |> resources("current", "vrf")
    |> Enum.reduce(%{}, fn %{"spec" => spec}, acc ->
      case spec["Name"] do
        "maeto-vrf-" <> tenant_id -> Map.put(acc, tenant_id, spec["TableID"])
        _ -> acc
      end
    end)
  end

  defp sids(tenants, state) do
    owners = Map.new(tenants, &{&1.dt46_sid, &1})

    desired = index(resources(state, "desired", "sid"))
    observed = index(resources(state, "current", "sid"))

    (Map.keys(desired) ++ Map.keys(observed))
    |> Enum.uniq()
    |> Enum.sort()
    |> Enum.map(fn address ->
      want = desired[address]
      got = observed[address]
      owner = owners[address]

      %{
        sid: address,
        tenant: owner && owner.id,
        expected_table: owner && owner.table_id,
        desired_table: want && want["TableID"],
        observed_table: got && got["TableID"],
        status: status(want, got)
      }
    end)
  end

  defp status(nil, _got), do: :orphan
  defp status(_want, nil), do: :missing

  defp status(want, got) do
    if want["TableID"] == got["TableID"], do: :ok, else: :wrong_vrf
  end

  defp duplicate_sids(nodes) do
    nodes
    |> Enum.flat_map(fn node ->
      for tenant <- node.tenants, tenant.dt46_sid, do: {tenant.dt46_sid, {node.id, tenant.id}}
    end)
    |> Enum.group_by(&elem(&1, 0), &elem(&1, 1))
    |> Enum.filter(fn {_sid, owners} -> length(Enum.uniq(owners)) > 1 end)
    |> Enum.map(fn {sid, owners} ->
      claimed = owners |> Enum.uniq() |> Enum.map_join(", ", fn {n, t} -> "#{n}/tenant #{t}" end)

      %{kind: :duplicate_sid, sid: sid, node: nil, detail: "claimed by #{claimed}"}
    end)
  end

  defp row_issues(nodes) do
    for node <- nodes, sid <- node.sids, sid.status != :ok do
      %{kind: sid.status, sid: sid.sid, node: node.id, detail: detail(sid)}
    end
  end

  defp detail(%{status: :orphan, observed_table: table}),
    do: "installed in vrf table #{table} but absent from the intent"

  defp detail(%{status: :missing, desired_table: table}),
    do: "expected in vrf table #{table} but not installed"

  defp detail(%{status: :wrong_vrf, desired_table: want, observed_table: got}),
    do: "installed in vrf table #{got}, intent says #{want}"

  defp resources(nil, _field, _kind), do: []

  defp resources(state, field, kind) do
    state
    |> Map.get(field)
    |> List.wrap()
    |> Enum.filter(&(&1["kind"] == kind))
  end

  defp index(list), do: Map.new(list, fn %{"spec" => spec} -> {spec["SID"], spec} end)

  defp get_in_safe(nil, _path), do: %{}

  defp get_in_safe(map, path), do: get_in(map, path) || %{}
end
