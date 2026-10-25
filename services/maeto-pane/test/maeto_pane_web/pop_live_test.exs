defmodule MaetoPaneWeb.PopLiveTest do
  use MaetoPaneWeb.ConnCase, async: false

  import Phoenix.LiveViewTest

  test "a pop drilldown renders its header and the chart container", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/pops/A")

    assert has_element?(view, "#pop-charts[phx-hook=ChartSync]")
    assert has_element?(view, "h1", "A")
  end

  test "the sidebar links back to pane sections", %{conn: conn} do
    {:ok, view, _html} = live(conn, "/pops/A")

    assert has_element?(view, ~s{#nav-links[href="/?section=links"]})
  end
end
