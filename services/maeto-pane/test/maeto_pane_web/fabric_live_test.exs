defmodule MaetoPaneWeb.FabricLiveTest do
  use MaetoPaneWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  @control %{
    "published_at" => "2026-08-29T00:00:00Z",
    "topology" => %{
      "domain" => %{},
      "prefixes" => [],
      "nodes" => [
        %{
          "id" => "A",
          "name" => "PopA",
          "locator" => "fc00:0:1::/48",
          "loopback" => "fc00:0:1::1/128"
        },
        %{
          "id" => "B",
          "name" => "PopB",
          "locator" => "fc00:0:2::/48",
          "loopback" => "fc00:0:2::1/128"
        }
      ],
      "edges" => [
        %{
          "id" => "A:eth1-B:eth1",
          "local" => "A",
          "remote" => "B",
          "role" => "link",
          "local_iface" => "eth1",
          "remote_iface" => "eth1",
          "local_addr" => "fc00:1:1:200::1/64",
          "remote_addr" => "fc00:1:1:200::2/64",
          "subnet" => "fc00:1:1:200::/64",
          "metric" => 0,
          "te_metric" => 0,
          "delay_ms" => 0,
          "up" => true
        },
        %{
          "id" => "A:eth2-B:eth2",
          "local" => "A",
          "remote" => "B",
          "role" => "link",
          "local_iface" => "eth2",
          "remote_iface" => "eth2",
          "local_addr" => "fc00:1:1:201::1/64",
          "remote_addr" => "fc00:1:1:201::2/64",
          "subnet" => "fc00:1:1:201::/64",
          "metric" => 0,
          "te_metric" => 0,
          "delay_ms" => 0,
          "up" => true
        }
      ]
    },
    "inventory" => [
      %{
        "id" => "A",
        "name" => "PopA",
        "index" => 1,
        "isis_net" => "49.0000.0000.0000.0001.00",
        "interfaces" => [
          %{
            "Name" => "eth1",
            "Role" => "core",
            "Peer" => "PopB",
            "Address" => "fc00:1:1:200::1/64"
          }
        ]
      }
    ]
  }

  @intent %{
    "node_type" => "pe",
    "generation" => 12,
    "intent" => %{
      "node_id" => "A",
      "tenants" => %{
        "273" => %{"dt46_sid" => "fc00:0:1:ff8c::", "portals" => %{"c" => %{}}}
      }
    }
  }

  defp sid(address, table) do
    %{
      "kind" => "sid",
      "key" => "dt46.#{address}.#{table}",
      "spec" => %{"SIDType" => "dt46", "SID" => address, "TableID" => table}
    }
  end

  defp vrf(tenant, table) do
    %{
      "kind" => "vrf",
      "key" => "maeto-vrf-#{tenant}",
      "spec" => %{"Name" => "maeto-vrf-#{tenant}", "TableID" => table}
    }
  end

  @state %{
    "node_id" => "A",
    "generation" => 12,
    "converged" => false,
    "passes" => 3,
    "desired" => [],
    "current" => []
  }

  setup do
    state = %{
      @state
      | "desired" => [vrf("273", 3_846_821_851), sid("fc00:0:1:ff8c::", 3_846_821_851)],
        "current" => [vrf("273", 3_846_821_851), sid("fc00:0:1:ff8c::", 4_112_525_701)]
    }

    :sys.replace_state(MaetoPane.Fabric, fn s ->
      Map.merge(s, %{intents: %{}, states: %{}, control: nil})
    end)

    send(MaetoPane.Fabric, {:kv, :control, :key_added, "snapshot", Jason.encode!(@control)})
    send(MaetoPane.Fabric, {:kv, :intents, :key_added, "pop.A", Jason.encode!(@intent)})
    send(MaetoPane.Fabric, {:kv, :states, :key_added, "pop.A", Jason.encode!(state)})

    :sys.get_state(MaetoPane.Fabric)

    :ok
  end

  test "survives a hot reload that left stale state behind", %{conn: conn} do
    :sys.replace_state(MaetoPane.Fabric, &Map.delete(&1, :control))

    {:ok, _view, html} = live(conn, "/")

    assert html =~ "no control snapshot yet"
  end

  test "a node that reported but could not read the dataplane says so", %{conn: conn} do
    blind = %{
      "node_id" => "A",
      "generation" => 12,
      "observed" => false,
      "converged" => false,
      "passes" => 1,
      "error" => "dataplane failure: couldn't retrieve vrf links",
      "desired" => [],
      "current" => []
    }

    send(MaetoPane.Fabric, {:kv, :states, :key_added, "pop.A", Jason.encode!(blind)})
    :sys.get_state(MaetoPane.Fabric)

    {:ok, view, _html} = live(conn, "/")

    rendered = render_click(view, "select", %{"kind" => "node", "id" => "A"})

    assert rendered =~ "could not read the dataplane"
    assert rendered =~ "couldn&#39;t retrieve vrf links"
    refute rendered =~ "no state reported"
  end

  test "a node with no state at all reports nothing", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    rendered = render_click(view, "select", %{"kind" => "node", "id" => "B"})

    assert rendered =~ "no state reported"
  end

  test "the registry is always visible without any selection", %{conn: conn} do
    control =
      Map.put(@control, "registry", %{
        "sid_cursor" => 4,
        "allocated_sids" => ["fc00:0:1:f4e0::", "fc00:0:1:fa36::", "fc00:0:1:ff8c::"],
        "sids_by_tenant" => %{
          "fc00:0:1::.231.dt46" => "fc00:0:1:fa36::",
          "fc00:0:1::.273.dt46" => "fc00:0:1:ff8c::"
        },
        "nodes" => %{
          "A" => %{
            "node_type" => "pe",
            "generation" => 12,
            "intent" => %{
              "node_id" => "A",
              "tenants" => %{
                "273" => %{"dt46_sid" => "fc00:0:1:ff8c::", "portals" => %{"c" => %{}}}
              }
            }
          }
        }
      })

    send(MaetoPane.Fabric, {:kv, :control, :key_added, "snapshot", Jason.encode!(control)})
    :sys.get_state(MaetoPane.Fabric)

    {:ok, _view, html} = live(conn, "/")

    assert html =~ "Service registry"
    assert html =~ "sid cursor 4"
    assert html =~ "3 sid(s) allocated"
    assert html =~ "fc00:0:1::"
    assert html =~ "231"
  end

  test "registry drifting from the published intent is flagged", %{conn: conn} do
    control =
      Map.put(@control, "registry", %{
        "sid_cursor" => 5,
        "allocated_sids" => [],
        "sids_by_tenant" => %{},
        "nodes" => %{
          "A" => %{
            "node_type" => "pe",
            "generation" => 13,
            "intent" => %{
              "node_id" => "A",
              "tenants" => %{
                "273" => %{"dt46_sid" => "fc00:0:1:aaaa::", "portals" => %{"c" => %{}}}
              }
            }
          }
        }
      })

    send(MaetoPane.Fabric, {:kv, :control, :key_added, "snapshot", Jason.encode!(control)})
    :sys.get_state(MaetoPane.Fabric)

    {:ok, _view, html} = live(conn, "/")

    assert html =~ "intent_drift"
    assert html =~ "registry holds fc00:0:1:aaaa:: for tenant 273"
    assert html =~ "published intent says fc00:0:1:ff8c::"
  end

  test "renders one circle per node and one line per node pair", %{conn: conn} do
    {:ok, _view, html} = live(conn, "/")

    assert length(Regex.scan(~r/<circle/, html)) == 2
    assert length(Regex.scan(~r/<path/, html)) == 2
  end

  test "selecting a node shows its detail panel", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    rendered =
      view
      |> element(~s{circle[phx-value-kind="node"][phx-value-id="A"]})
      |> render_click()

    assert rendered =~ "PopA"
    assert rendered =~ "fc00:0:1::/48"
    assert rendered =~ "49.0000.0000.0000.0001.00"
    assert rendered =~ "wrong_vrf"
  end

  test "selecting a link lists its parallel edges", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    assert view |> has_element?(~s{path[phx-value-id="A|B"]})

    rendered = render_click(view, "select", %{"kind" => "link", "id" => "A|B"})

    assert rendered =~ "2 parallel link"
    assert rendered =~ "A:eth1-B:eth1"
    assert rendered =~ "A:eth2-B:eth2"
    assert rendered =~ "fc00:1:1:201::/64"
  end

  test "clearing the selection returns to the issue list", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/")

    selected = view |> element(~s{circle[phx-value-id="A"]}) |> render_click()
    assert selected =~ "49.0000.0000.0000.0001.00"

    cleared = view |> element("svg") |> render_click()

    refute cleared =~ "49.0000.0000.0000.0001.00"
    assert cleared =~ "installed in vrf table 4112525701"
  end
end
