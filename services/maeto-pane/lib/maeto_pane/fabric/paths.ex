defmodule MaetoPane.Fabric.Paths do
  @moduledoc """
  The service layer of a snapshot: the segment lists each PE was told to install,
  what the kernel actually reports, and the customer sites they carry.
  """

  alias MaetoPane.Fabric.Net

  @latency "latency"

  def index(snapshot) do
    control = Map.get(snapshot, :control) || %{}

    (get_in(control, ["topology", "nodes"]) || [])
    |> Enum.flat_map(fn node ->
      case Net.parse_prefix(node["locator"] || "") do
        {:ok, prefix} ->
          {base, _bits} = prefix

          [%{id: node["id"], name: node["name"], prefix: prefix, base: base}]

        :error ->
          []
      end
    end)
  end

  @doc "Every CSPF path the controller last computed, keyed by {source, dest}."
  def computed(snapshot) do
    control = Map.get(snapshot, :control) || %{}

    (control["paths"] || [])
    |> Enum.map(fn path ->
      %{
        source: path["source"],
        dest: path["dest"],
        dimension: path["dimension"],
        nodes: path["nodes"] || [],
        edges: path["edges"] || [],
        cost: path["cost"],
        hops: max(length(path["nodes"] || []) - 1, 0)
      }
    end)
    |> Enum.sort_by(&{&1.source, &1.dest, &1.dimension})
  end

  def computed_index(computed) do
    Map.new(computed, fn path -> {{path.source, path.dest, path.dimension}, path} end)
  end

  @doc """
  One row per segment list, over the union of what the controller asked for and
  what the node reports installed.
  """
  def rows(snapshot) do
    intents = Map.get(snapshot, :intents) || %{}
    states = Map.get(snapshot, :states) || %{}
    locators = index(snapshot)
    sites = sites(snapshot)
    costs = snapshot |> computed() |> computed_index()

    (Map.keys(intents) ++ Map.keys(states))
    |> Enum.uniq()
    |> Enum.filter(&String.starts_with?(&1, "pop."))
    |> Enum.sort()
    |> Enum.flat_map(fn key ->
      id = String.replace_prefix(key, "pop.", "")

      node_rows(id, Map.get(intents, key), Map.get(states, key), locators, sites, costs)
    end)
  end

  def issues(rows) do
    for row <- rows, row.status in [:missing, :orphan, :drifted] do
      %{kind: path_issue_kind(row.status), node: row.pe, sid: row.prefix, detail: detail(row)}
    end
  end

  def select(rows, id), do: Enum.find(rows, &(&1.id == id))

  @doc """
  The node chain a segment list walks. The controller leaves the ingress PE out of
  the segment list -- it is the node doing the encapsulating -- so put it back.
  """
  def chain(nil), do: []

  def chain(row) do
    [row.pe | Enum.map(row.hops, & &1.node)]
    |> Enum.reject(&is_nil/1)
    |> Enum.dedup()
  end

  @doc "Node pairs a highlighted path traverses, as {a, b} with a < b."
  def hop_pairs(row) do
    row
    |> chain()
    |> Enum.chunk_every(2, 1, :discard)
    |> Enum.map(fn [a, b] -> [a, b] |> Enum.sort() |> List.to_tuple() end)
  end

  @doc "Registered sites, each carrying whatever the portal has reported back."
  def sites(snapshot) do
    control = Map.get(snapshot, :control) || %{}
    intents = Map.get(snapshot, :intents) || %{}
    states = Map.get(snapshot, :states) || %{}

    for tenant <- control["tenants"] || [], site <- tenant["sites"] || [] do
      portal = site["portal_id"]
      intent = get_in(intents, ["cpe.#{portal}", "intent"])
      state = Map.get(states, "cpe.#{portal}")

      %{
        tenant: to_string(tenant["id"]),
        allocation: tenant["allocation"],
        vrf_table: tenant["vrf_table"],
        cpe: site["cpe"],
        node: site["node"],
        portal_id: portal,
        prefix: site["prefix"],
        attach: site["attach"],
        attach_node: site["attach_node"],
        if_id: site["if_id"],
        identity: site["identity"],
        tunnel_if: intent && intent["tunnel_interface_id"],
        reported_at: state && state["reported_at"],
        status: site_status(intent, state)
      }
    end
    |> Enum.sort_by(&{&1.tenant, &1.cpe})
  end

  def site_counts(sites) do
    %{total: length(sites), up: Enum.count(sites, &(&1.status == :up))}
  end

  def path_counts(rows) do
    %{
      total: length(rows),
      installed: Enum.count(rows, &(&1.status == :installed)),
      degraded: Enum.count(rows, &(&1.status in [:missing, :orphan, :drifted]))
    }
  end

  defp node_rows(id, intent, state, locators, sites, costs) do
    tables = vrf_tables(state)
    tenants_by_table = Map.new(tables, fn {tenant, table} -> {table, tenant} end)

    wanted = intent_paths(intent)
    planned = sr_routes(state, "desired", tenants_by_table)
    installed = sr_routes(state, "current", tenants_by_table)

    (Map.keys(wanted) ++ Map.keys(installed))
    |> Enum.uniq()
    |> Enum.sort()
    |> Enum.map(fn key ->
      sides = %{
        want: Map.get(wanted, key),
        planned: Map.get(planned, key),
        got: Map.get(installed, key),
        table: Map.get(tables, elem(key, 0))
      }

      row(id, key, sides, locators, sites, costs)
    end)
  end

  defp row(id, {tenant, prefix}, sides, locators, sites, costs) do
    %{want: want, got: got} = sides

    segments = first_of([want, got], :segments, [])
    hops = Enum.map(segments, &resolve(&1, locators))
    egress = List.last(hops) || %{node: nil, node_name: nil}

    %{
      kind: :path,
      id: "#{id}|#{tenant}|#{prefix}",
      pe: id,
      tenant: tenant,
      prefix: prefix,
      table: first_of([want, got], :table, sides.table),
      dest: egress.node,
      dest_name: egress.node_name,
      site: Enum.find(sites, &(&1.prefix == prefix)),
      segments: segments,
      hops: hops,
      observed: first_of([got], :segments, nil),
      cost: first_of([Map.get(costs, {id, egress.node, @latency})], :cost, nil),
      status: status(want, sides.planned, got)
    }
  end

  defp first_of(entries, key, fallback) do
    Enum.find_value(entries, fallback, fn entry -> entry && Map.get(entry, key) end)
  end

  defp intent_paths(nil), do: %{}

  defp intent_paths(intent) do
    for {tenant, held} <- get_in(intent, ["intent", "tenants"]) || %{},
        install <- held["install_paths"] || [],
        prefix <- install["prefix_route"] || [],
        into: %{} do
      {{tenant, prefix}, %{segments: install["segments"] || [], table: nil}}
    end
  end

  defp sr_routes(nil, _field, _tenants), do: %{}

  defp sr_routes(state, field, tenants) do
    state
    |> Map.get(field)
    |> List.wrap()
    |> Enum.filter(&(&1["kind"] == "sr_route"))
    |> Map.new(fn %{"spec" => spec} ->
      table = spec["Table"]
      tenant = Map.get(tenants, table, "table #{table}")

      # the reconciler reverses a segment list on its way into netlink, so both
      # sides of the diff are stored tail-first
      {{tenant, spec["Prefix"]}, %{segments: Enum.reverse(spec["Segments"] || []), table: table}}
    end)
  end

  defp status(nil, _planned, nil), do: :unknown
  defp status(nil, _planned, _got), do: :orphan
  defp status(_want, nil, nil), do: :pending
  defp status(_want, _planned, nil), do: :missing

  defp status(want, _planned, got) do
    if want.segments == got.segments, do: :installed, else: :drifted
  end

  defp resolve(sid, locators) do
    with {:ok, address} <- Net.parse_addr(sid),
         %{} = owner <- Enum.find(locators, &Net.contains?(&1.prefix, address)) do
      %{
        sid: sid,
        node: owner.id,
        node_name: owner.name,
        role: if(address == owner.base, do: :transit, else: :decap)
      }
    else
      _ -> %{sid: sid, node: nil, node_name: nil, role: :unknown}
    end
  end

  defp vrf_tables(nil), do: %{}

  defp vrf_tables(state) do
    state
    |> Map.get("current")
    |> List.wrap()
    |> Enum.filter(&(&1["kind"] == "vrf"))
    |> Enum.reduce(%{}, fn %{"spec" => spec}, acc ->
      case spec["Name"] do
        "maeto-vrf-" <> tenant -> Map.put(acc, tenant, spec["TableID"])
        _ -> acc
      end
    end)
  end

  defp site_status(nil, _state), do: :offline

  defp site_status(intent, state) do
    cond do
      (intent["tunnel_interface_id"] || 0) == 0 -> :no_tunnel
      is_nil(state) -> :no_report
      state["converged"] -> :up
      true -> :converging
    end
  end

  defp path_issue_kind(:missing), do: :path_not_installed
  defp path_issue_kind(:orphan), do: :path_orphaned
  defp path_issue_kind(:drifted), do: :path_drift

  defp detail(%{status: :missing, pe: pe, dest_name: dest}),
    do: "#{pe} rendered the segment list towards #{dest || "?"} but the kernel has no route"

  defp detail(%{status: :orphan, pe: pe}),
    do: "#{pe} still carries this segment list, but no intent asks for it"

  defp detail(%{status: :drifted, pe: pe, segments: want, observed: got}),
    do: "#{pe} installed #{Enum.join(got, " > ")}, intent says #{Enum.join(want, " > ")}"
end
