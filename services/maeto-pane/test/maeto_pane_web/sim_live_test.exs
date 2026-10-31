defmodule MaetoPaneWeb.SimLiveTest do
  use MaetoPaneWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  defp publish(state) do
    send(MaetoPane.Sim, {:sim, :key_added, "state", Jason.encode!(state)})
    :sys.get_state(MaetoPane.Sim)
  end

  defp state(overrides) do
    now = System.os_time(:millisecond) / 1000

    Map.merge(
      %{
        "updated_at" => now,
        "error" => nil,
        "limits" => %{
          "delay_ms" => 200,
          "jitter_ms" => 50,
          "loss_pct" => 30,
          "rate_mbps" => 1000,
          "ttl_max_s" => 600,
          "manual_faults" => 4,
          "manual_down" => 2,
          "scenario_max_s" => 900
        },
        "links" => [
          %{
            "id" => "AB1",
            "label" => "A–B #1",
            "a" => "A",
            "b" => "B",
            "a_iface" => "eth1",
            "b_iface" => "eth1",
            "bandwidth_mbps" => 140,
            "delay_ms" => 4.0
          },
          %{
            "id" => "CE1",
            "label" => "C–E",
            "a" => "C",
            "b" => "E",
            "a_iface" => "eth3",
            "b_iface" => "eth1",
            "bandwidth_mbps" => 140,
            "delay_ms" => 9.0
          }
        ],
        "faults" => [
          %{
            "link" => "AB1",
            "kind" => "impair",
            "source" => "manual",
            "direction" => "ab",
            "started_at" => now,
            "expires_at" => now + 90,
            "delay_ms" => 40.0,
            "jitter_ms" => 0,
            "loss_pct" => 2.5,
            "rate_mbps" => nil
          },
          %{
            "link" => "CE1",
            "kind" => "down",
            "source" => "story:2-fiber-cut",
            "direction" => "both",
            "started_at" => now,
            "expires_at" => now + 30
          }
        ],
        "scenarios" => [
          %{
            "name" => "random",
            "title" => "Customer day",
            "description" => "d",
            "duration_s" => 0
          }
        ],
        "scenario" => %{
          "name" => "random",
          "owner" => "manual",
          "started_at" => now,
          "expires_at" => now + 600,
          "report" => " t=  10s  day 08:08"
        },
        "last_scenario" => nil,
        "stories" => [
          %{
            "name" => "2-fiber-cut",
            "title" => "Fiber cut on C–E",
            "description" => "d",
            "duration_s" => 120
          }
        ],
        "story" => %{
          "name" => "2-fiber-cut",
          "title" => "Fiber cut on C–E",
          "started_at" => now,
          "ends_at" => now + 120
        }
      },
      overrides
    )
  end

  test "renders stories, traffic and links with their faults", %{conn: conn} do
    publish(state(%{}))

    {:ok, view, html} = live(conn, "/sim")

    assert html =~ "Simulator live"
    assert has_element?(view, "#story-2-fiber-cut", "Stop and revert")
    assert has_element?(view, "#scenario-random", "running")
    assert has_element?(view, "#sim-link-AB1", "+40 ms · 2.5% loss")
    assert has_element?(view, "#sim-link-AB1", "A → B")
    assert has_element?(view, "#sim-link-CE1", "Down")
    assert has_element?(view, "#nav-sim[href=\"/sim\"]")
  end

  test "selecting a link opens its impair and cut forms", %{conn: conn} do
    publish(state(%{}))

    {:ok, view, _html} = live(conn, "/sim")

    view |> element("#sim-link-CE1") |> render_click()

    assert has_element?(view, "#impair-form")
    assert has_element?(view, "#down-form")
    assert has_element?(view, "#inspector", "Revert now")
  end

  test "says when the simulator is not publishing", %{conn: conn} do
    publish(state(%{"updated_at" => 0, "faults" => [], "scenario" => nil, "story" => nil}))

    {:ok, _view, html} = live(conn, "/sim")

    assert html =~ "Simulator offline"
    assert html =~ "maeto-sim is not publishing"
  end
end
