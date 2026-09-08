defmodule MaetoPaneWeb.FabricLivePathsTest do
  use MaetoPaneWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  defp graph_payload(html) do
    html
    |> LazyHTML.from_fragment()
    |> LazyHTML.query_by_id("topology")
    |> LazyHTML.attribute("data-graph")
    |> List.first()
    |> Jason.decode!()
  end

  @prefix "fd7a:3921:111:2::/64"
  @table 273
  @segments ["fc00:0:2::", "fc00:0:3:f4e0::"]

  @control %{
    "published_at" => "2026-09-07T18:04:11Z",
    "topology" => %{
      "domain" => %{},
      "prefixes" => [],
      "nodes" => [
        %{"id" => "A", "name" => "PopA", "locator" => "fc00:0:1::/48"},
        %{"id" => "B", "name" => "PopB", "locator" => "fc00:0:2::/48"},
        %{"id" => "C", "name" => "PopC", "locator" => "fc00:0:3::/48"}
      ],
      "edges" => [
        %{
          "id" => "A:eth2-B:eth2",
          "local" => "A",
          "remote" => "B",
          "up" => true,
          "metric" => 10,
          "te_metric" => 0,
          "delay_ms" => 2
        },
        %{
          "id" => "B:eth4-C:eth4",
          "local" => "B",
          "remote" => "C",
          "up" => true,
          "metric" => 10,
          "te_metric" => 0,
          "delay_ms" => 3
        }
      ]
    },
    "inventory" => [],
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
        "cost" => 5.0
      }
    ]
  }

  @intent %{
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
              "segments" => @segments,
              "color" => 0
            }
          ]
        }
      }
    }
  }

  defp sr_route(segments) do
    %{
      "kind" => "sr_route",
      "key" => "#{@table}.#{@prefix}",
      "spec" => %{
        "Prefix" => @prefix,
        "Segments" => Enum.reverse(segments),
        "Table" => @table,
        "Color" => 0
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

  defp state(current) do
    %{
      "node_id" => "A",
      "generation" => 12,
      "converged" => true,
      "passes" => 1,
      "desired" => [vrf(), sr_route(@segments)],
      "current" => [vrf() | current]
    }
  end

  defp seed(control, intents, states) do
    :sys.replace_state(MaetoPane.Fabric, fn s ->
      Map.merge(s, %{intents: intents, states: states, control: control, connected: true})
    end)

    :sys.get_state(MaetoPane.Fabric)

    :ok
  end

  setup do
    seed(
      @control,
      %{
        "pop.A" => @intent,
        "cpe.aa400723fee3" => %{"intent" => %{"tunnel_interface_id" => 3}}
      },
      %{
        "pop.A" => state([sr_route(@segments)]),
        "cpe.aa400723fee3" => %{"converged" => true}
      }
    )
  end

  test "the segment list section is the landing view and shows the whole chain", %{conn: conn} do
    {:ok, _view, html} = live(conn, "/")

    assert html =~ "Segment lists"
    assert html =~ "PopC"
    assert html =~ "fc00:0:2::"
    assert html =~ "fc00:0:3:f4e0::"
    assert html =~ @prefix
    assert html =~ "Reconciled"
  end

  test "the title line tallies adjacencies, segment lists, sites and issues", %{conn: conn} do
    {:ok, _view, html} = live(conn, "/")

    assert html =~ "adjacencies up"
    assert html =~ "segment lists in FIB"
    assert html =~ "sites up"
    assert html =~ "issues"
    assert html =~ "PoP mesh"
  end

  test "selecting a segment list opens it in the inspector", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    rendered =
      view
      |> element(~s{tr[phx-value-kind="path"][phx-value-id="A|273|#{@prefix}"]})
      |> render_click()

    assert rendered =~ "end.dt46"
    assert rendered =~ "Segment list"
    assert rendered =~ "PopB"
  end

  test "selecting a segment list traces its hops on the topology", %{conn: conn} do
    {:ok, view, html} = live(conn, "/")

    refute html =~ "stroke-accent"

    rendered = render_click(view, "select", %{"kind" => "path", "id" => "A|273|#{@prefix}"})

    # A > B > C: the renderer is handed the chain and the overlay flips to path
    graph = graph_payload(rendered)

    assert graph["trace"] == ["A", "B", "C"]
    assert graph["overlay"] == "path"
    assert graph["selection"] == %{"kind" => "path", "id" => "A|273|#{@prefix}"}
    assert rendered =~ "A › B › C"
  end

  test "a drifted segment list shows what the fib holds instead", %{conn: conn} do
    moved = ["fc00:0:4::", "fc00:0:3:f4e0::"]

    seed(
      @control,
      %{"pop.A" => @intent},
      %{"pop.A" => state([sr_route(moved)])}
    )

    {:ok, view, html} = live(conn, "/")

    assert html =~ "Drift"
    assert html =~ "path_drift"

    rendered = render_click(view, "select", %{"kind" => "path", "id" => "A|273|#{@prefix}"})

    assert rendered =~ "Desired against actual"
    assert rendered =~ "Observed FIB"
    assert rendered =~ "the kernel holds a different segment list"
    assert rendered =~ "fc00:0:4::"
  end

  test "registered sites are listed and selectable", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    html = view |> element("#nav-sites") |> render_click()

    assert html =~ "cA"
    assert html =~ "83505cf19a08"

    rendered =
      view
      |> element(~s{tr[phx-value-kind="site"][phx-value-id="aa400723fee3"]})
      |> render_click()

    assert rendered =~ "aa400723fee3.cpe.maeto.net"
    assert rendered =~ "fd7a:3921:111::/48"
  end

  test "the computed path set is a section of its own", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    html = view |> element("#nav-computed") |> render_click()

    assert html =~ "Computed paths"
    assert html =~ "latency"
    assert html =~ "5.0"
    # the pair carries a programmed segment list, so the reasoning ties to the result
    assert html =~ "segment list"
  end

  test "the links section reports each adjacency with its cost", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    html = view |> element("#nav-links") |> render_click()

    assert html =~ "Adjacency"
    assert html =~ "not yet per direction"
  end

  test "the cost overlay labels every link with its latency cost", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    html = view |> element("#overlay-cost") |> render_click()

    assert html =~ "thicker is costlier"
    # the cost is carried per edge so the renderer can step one hue by magnitude
    assert graph_payload(html)["overlay"] == "cost"
    assert Enum.map(graph_payload(html)["edges"], & &1["cost"]) == [2, 3]
  end

  test "sites attached to a pop ride along for the map badge", %{conn: conn} do
    {:ok, _view, html} = live(conn, "/")

    counts = Map.new(graph_payload(html)["nodes"], &{&1["id"], &1["sites"]})

    assert counts == %{"A" => 1, "B" => 0, "C" => 1}
  end

  test "an empty control snapshot renders empty states rather than crashing", %{conn: conn} do
    seed(nil, %{}, %{})

    {:ok, view, html} = live(conn, "/")

    assert html =~ "no snapshot"
    assert graph_payload(html)["nodes"] == []
    assert html =~ "No segment list programmed yet."

    assert view |> element("#nav-sites") |> render_click() =~
             "No tenant database in the control snapshot."

    assert view |> element("#nav-computed") |> render_click() =~
             "The PCE has not published a path set yet."

    assert view |> element("#nav-links") |> render_click() =~
             "No adjacency in the control snapshot."
  end
end
