defmodule MaetoPane.Pane.Fib do
  @moduledoc """
  What an agent reports as installed, grouped by the table it lives in: main,
  the mirror table, the plr table, every tenant vrf with its class tables, and
  the standby tables of the sites this node protects. Triggers are listed as
  armed; whether one has fired is not reported.
  """

  import Bitwise

  @main_table 254
  @plr_table 950
  @vrf_table_bits 24
  @vrf_table_mask (1 <<< @vrf_table_bits) - 1

  def build(nil), do: %{tables: [], triggers: []}

  def build(state) do
    current = List.wrap(state["current"])
    vrfs = vrf_names(current)
    mirror = mirror_table(current)
    name = &table_name(&1, vrfs, mirror)

    entries =
      Enum.flat_map(current, fn
        %{"kind" => "route", "spec" => spec} -> [route(spec, name)]
        %{"kind" => "sr_route", "spec" => spec} -> [sr_route(spec, name)]
        %{"kind" => "sid", "spec" => spec} -> [sid(spec, name)]
        _ -> []
      end)

    tables =
      entries
      |> Enum.group_by(& &1.table)
      |> Enum.map(fn {table, rows} ->
        %{name: table, rows: Enum.sort_by(rows, & &1.match)}
      end)
      |> Enum.sort_by(&table_order/1)

    %{tables: tables, triggers: triggers(current, name)}
  end

  defp vrf_names(current) do
    for %{"kind" => "vrf", "spec" => spec} <- current, into: %{} do
      {spec["TableID"], spec["Name"]}
    end
  end

  # the mirror table is whatever this node's End.M decaps into
  defp mirror_table(current) do
    Enum.find_value(current, fn
      %{"kind" => "sid", "spec" => %{"SIDType" => "dt6", "VrfTableID" => table}} -> table
      _ -> nil
    end)
  end

  defp table_name(table, _vrfs, _mirror) when table in [nil, 0, @main_table], do: "main"
  defp table_name(@plr_table, _vrfs, _mirror), do: "plr (950)"
  defp table_name(table, _vrfs, table) when not is_nil(table), do: "mirror"

  defp table_name(table, vrfs, _mirror) do
    color = table >>> @vrf_table_bits
    vrf = Map.get(vrfs, table &&& @vrf_table_mask)

    cond do
      color == 0 and vrf -> vrf
      vrf -> "#{vrf} · color #{color}"
      true -> to_string(table)
    end
  end

  defp table_order(%{name: "main"}), do: {0, ""}
  defp table_order(%{name: "mirror"}), do: {1, ""}
  defp table_order(%{name: "plr (950)"}), do: {2, ""}
  defp table_order(%{name: "maeto-vrf-" <> _ = name}), do: {3, name}
  defp table_order(%{name: "standby-" <> _ = name}), do: {4, name}
  defp table_order(%{name: name}), do: {5, name}

  defp route(spec, name) do
    via = if spec["Via"] in [nil, ""], do: "", else: " via #{spec["Via"]}"

    %{
      table: name.(spec["Table"]),
      match: spec["Dst"],
      action: "dev #{spec["Dev"]}#{via}",
      kind: "route"
    }
  end

  # the reconciler stores segment lists tail-first, as netlink takes them
  defp sr_route(spec, name) do
    segments = spec["Segments"] |> List.wrap() |> Enum.reverse() |> Enum.join(" → ")

    %{
      table: name.(spec["Table"]),
      match: spec["Prefix"],
      action: "encap [#{segments}]",
      kind: "srv6"
    }
  end

  defp sid(spec, name) do
    %{
      table: name.(spec["TableID"]),
      match: spec["SID"],
      action: sid_action(spec, name),
      kind: spec["SIDType"]
    }
  end

  defp sid_action(%{"SIDType" => "dt6", "VrfTableID" => table}, name),
    do: "End.M → #{name.(table)}"

  defp sid_action(%{"SIDType" => "dt46", "VrfTableID" => table}, name),
    do: "End.DT46 → #{name.(table)}"

  defp sid_action(%{"SIDType" => "end"}, _name), do: "End"
  defp sid_action(%{"SIDType" => type}, _name), do: type

  defp triggers(current, name) do
    for %{"kind" => "rule_trigger", "spec" => spec} <- current do
      %{
        egress: spec["Egress"],
        rules: spec["Rules"] |> List.wrap() |> Enum.map(&trigger_rule(&1, name))
      }
    end
    |> Enum.sort_by(& &1.egress)
  end

  defp trigger_rule(rule, name) do
    match =
      [{"from", rule["Src"]}, {"to", rule["Dst"]}]
      |> Enum.reject(fn {_word, prefix} -> prefix in [nil, ""] end)
      |> Enum.map_join(" ", fn {word, prefix} -> "#{word} #{prefix}" end)

    "#{rule["Priority"]}: #{match} lookup #{name.(rule["Table"])}"
  end
end
