defmodule MaetoPane.Fabric.PathsTest do
  use ExUnit.Case, async: true

  alias MaetoPane.Fabric.Paths

  @prefix "fd7a:3921:111:2::/64"
  @table 273
  # what the controller asks for, ingress-first
  @segments ["fc00:0:2::", "fc00:0:3:f4e0::"]

  defp control do
    %{
      "topology" => %{
        "nodes" => [
          %{"id" => "A", "name" => "PopA", "locator" => "fc00:0:1::/48"},
          %{"id" => "B", "name" => "PopB", "locator" => "fc00:0:2::/48"},
          %{"id" => "C", "name" => "PopC", "locator" => "fc00:0:3::/48"}
        ],
        "edges" => []
      },
      "tenants" => [
        %{
          "id" => 273,
          "allocation" => "fd7a:3921:111::/48",
          "vrf_table" => @table,
          "sites" => [
            %{
              "cpe" => "cA",
              "portal_id" => "83505cf19a08",
              "node" => "CpeA",
              "prefix" => "fd7a:3921:111:1::/64",
              "attach" => "A",
              "attach_node" => "PopA",
              "if_id" => 2,
              "identity" => "83505cf19a08.cpe.maeto.net"
            },
            %{
              "cpe" => "cC",
              "portal_id" => "aa400723fee3",
              "node" => "CpeC",
              "prefix" => @prefix,
              "attach" => "C",
              "attach_node" => "PopC",
              "if_id" => 3,
              "identity" => "aa400723fee3.cpe.maeto.net"
            }
          ]
        }
      ],
      "paths" => [
        %{
          "source" => "A",
          "dest" => "C",
          "dimension" => "latency",
          "nodes" => ["A", "B", "C"],
          "edges" => ["A:eth2-B:eth2", "B:eth4-C:eth4"],
          "cost" => 12.5
        }
      ]
    }
  end

  defp intent(segments) do
    %{
      "node_type" => "pe",
      "generation" => 12,
      "intent" => %{
        "node_id" => "A",
        "tenants" => %{
          "273" => %{
            "dt46_sid" => "fc00:0:1:ff8c::",
            "portals" => %{},
            "install_paths" => [
              %{
                "tenant_id" => "273",
                "prefix_route" => [@prefix],
                "segments" => segments,
                "color" => 0
              }
            ]
          }
        }
      }
    }
  end

  defp vrf do
    %{
      "kind" => "vrf",
      "key" => "maeto-vrf-273",
      "spec" => %{"Name" => "maeto-vrf-273", "TableID" => @table}
    }
  end

  # the reconciler reverses the list on its way into netlink, so the fib carries
  # it tail-first and so does the state report
  defp sr_route(segments, prefix \\ @prefix) do
    %{
      "kind" => "sr_route",
      "key" => "#{@table}.#{prefix}",
      "spec" => %{
        "Prefix" => prefix,
        "Segments" => Enum.reverse(segments),
        "Table" => @table,
        "Color" => 0
      }
    }
  end

  defp snapshot(opts) do
    %{
      connected: true,
      control: control(),
      intents: Keyword.get(opts, :intents, %{}),
      states: Keyword.get(opts, :states, %{})
    }
  end

  defp state(desired, current) do
    %{
      "node_id" => "A",
      "generation" => 12,
      "converged" => true,
      "passes" => 1,
      "desired" => [vrf() | desired],
      "current" => [vrf() | current]
    }
  end

  defp installed_snapshot do
    snapshot(
      intents: %{"pop.A" => intent(@segments)},
      states: %{"pop.A" => state([sr_route(@segments)], [sr_route(@segments)])}
    )
  end

  describe "rows/1" do
    test "a segment list in both the intent and the fib reads as installed" do
      assert [row] = Paths.rows(installed_snapshot())

      assert row.pe == "A"
      assert row.tenant == "273"
      assert row.prefix == @prefix
      assert row.table == @table
      assert row.status == :installed
      assert row.segments == @segments
    end

    test "each segment resolves to the pop whose locator minted it" do
      [row] = Paths.rows(installed_snapshot())

      assert Enum.map(row.hops, & &1.node) == ["B", "C"]
      assert Enum.map(row.hops, & &1.node_name) == ["PopB", "PopC"]
      assert row.dest == "C"
      assert row.dest_name == "PopC"
    end

    test "the last segment is flagged as the decap sid, the rest as transit" do
      [row] = Paths.rows(installed_snapshot())

      assert Enum.map(row.hops, & &1.role) == [:transit, :decap]
    end

    test "the row carries the destination site and the computed cost" do
      [row] = Paths.rows(installed_snapshot())

      assert row.site.cpe == "cC"
      assert row.cost == 12.5
    end

    test "an intent the node has not rendered yet is pending" do
      snapshot = snapshot(intents: %{"pop.A" => intent(@segments)})

      assert [%{status: :pending}] = Paths.rows(snapshot)
    end

    test "rendered but absent from the fib is missing" do
      snapshot =
        snapshot(
          intents: %{"pop.A" => intent(@segments)},
          states: %{"pop.A" => state([sr_route(@segments)], [])}
        )

      assert [%{status: :missing}] = Paths.rows(snapshot)
    end

    test "a fib entry no intent asks for is an orphan" do
      snapshot = snapshot(states: %{"pop.A" => state([], [sr_route(@segments)])})

      assert [%{status: :orphan}] = Paths.rows(snapshot)
    end

    test "different segments for the same prefix is drift, not a second row" do
      moved = ["fc00:0:4::", "fc00:0:3:f4e0::"]

      snapshot =
        snapshot(
          intents: %{"pop.A" => intent(@segments)},
          states: %{"pop.A" => state([sr_route(@segments)], [sr_route(moved)])}
        )

      assert [row] = Paths.rows(snapshot)
      assert row.status == :drifted
      assert row.segments == @segments
      assert row.observed == moved
    end

    test "an unresolvable segment does not take the row down with it" do
      snapshot = snapshot(intents: %{"pop.A" => intent(["fd00:dead::", "fc00:0:3:f4e0::"])})

      assert [row] = Paths.rows(snapshot)
      assert [%{node: nil, role: :unknown}, %{node: "C"}] = row.hops
      assert row.dest == "C"
    end
  end

  describe "issues/1" do
    test "only a degraded row raises an issue" do
      assert Paths.issues(Paths.rows(installed_snapshot())) == []
    end

    test "drift explains both sides" do
      moved = ["fc00:0:4::", "fc00:0:3:f4e0::"]

      snapshot =
        snapshot(
          intents: %{"pop.A" => intent(@segments)},
          states: %{"pop.A" => state([sr_route(@segments)], [sr_route(moved)])}
        )

      assert [issue] = snapshot |> Paths.rows() |> Paths.issues()
      assert issue.kind == :path_drift
      assert issue.node == "A"
      assert issue.detail =~ "fc00:0:4:: > fc00:0:3:f4e0::"
      assert issue.detail =~ "intent says fc00:0:2:: > fc00:0:3:f4e0::"
    end
  end

  describe "chain/1 and hop_pairs/1" do
    test "the ingress pe is put back at the head of the chain" do
      [row] = Paths.rows(installed_snapshot())

      assert Paths.chain(row) == ["A", "B", "C"]
      assert Paths.hop_pairs(row) == [{"A", "B"}, {"B", "C"}]
    end
  end

  describe "sites/1" do
    test "every registered site shows up, offline until its portal checks in" do
      sites = Paths.sites(snapshot([]))

      assert Enum.map(sites, & &1.cpe) == ["cA", "cC"]
      assert Enum.all?(sites, &(&1.status == :offline))
    end

    test "a portal with no tunnel yet is distinguished from one that is up" do
      intents = %{
        "cpe.83505cf19a08" => %{"intent" => %{"tunnel_interface_id" => 0}},
        "cpe.aa400723fee3" => %{"intent" => %{"tunnel_interface_id" => 3}}
      }

      states = %{"cpe.aa400723fee3" => %{"converged" => true}}

      sites = Paths.sites(snapshot(intents: intents, states: states))

      assert %{cpe: "cA", status: :no_tunnel} = Enum.find(sites, &(&1.cpe == "cA"))
      assert %{cpe: "cC", status: :up, tunnel_if: 3} = Enum.find(sites, &(&1.cpe == "cC"))
    end

    test "an unreported portal counts registered but not up" do
      intents = %{"cpe.aa400723fee3" => %{"intent" => %{"tunnel_interface_id" => 3}}}
      sites = Paths.sites(snapshot(intents: intents))

      assert %{status: :no_report} = Enum.find(sites, &(&1.cpe == "cC"))
      assert Paths.site_counts(sites) == %{total: 2, up: 0}
    end
  end

  describe "computed/1" do
    test "the published path set is exposed with a hop count" do
      assert [path] = Paths.computed(snapshot([]))

      assert path.source == "A"
      assert path.dest == "C"
      assert path.nodes == ["A", "B", "C"]
      assert path.hops == 2
      assert path.cost == 12.5
    end

    test "an empty snapshot yields nothing rather than raising" do
      assert Paths.computed(%{}) == []
      assert Paths.rows(%{}) == []
      assert Paths.sites(%{}) == []
    end
  end
end
