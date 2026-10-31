defmodule MaetoPaneWeb.Pane.PceStatusTest do
  use ExUnit.Case, async: true

  import Phoenix.LiveViewTest

  alias MaetoPaneWeb.Pane.UI

  defp readiness(overrides) do
    Map.merge(
      %{
        "ready" => true,
        "timed_out" => false,
        "converged" => 36,
        "edges" => 36,
        "deadline" => "2026-10-31T12:05:00Z"
      },
      overrides
    )
  end

  test "renders nothing before a controller snapshot carries readiness" do
    assert render_component(&UI.pce_status/1, readiness: nil) == ""
  end

  test "shows warming up with converged links while the PCE waits" do
    html =
      render_component(&UI.pce_status/1,
        readiness: readiness(%{"ready" => false, "converged" => 12})
      )

    assert html =~ "PCE warming up · 12/36"
    assert html =~ "bg-warn-soft"
  end

  test "shows ready once every link has converged" do
    html = render_component(&UI.pce_status/1, readiness: readiness(%{}))

    assert html =~ "PCE ready"
    assert html =~ "bg-ok-soft"
  end

  test "flags a ready PCE that stopped waiting on unsettled links" do
    html =
      render_component(&UI.pce_status/1,
        readiness: readiness(%{"timed_out" => true, "converged" => 30})
      )

    assert html =~ "PCE ready · 30/36 converged"
    assert html =~ "bg-warn-soft"
  end
end
