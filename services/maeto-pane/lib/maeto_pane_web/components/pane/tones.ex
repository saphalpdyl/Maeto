defmodule MaetoPaneWeb.Pane.Tones do
  @moduledoc """
  Maps operational state to the console's semantic tones and labels, so a
  status reads the same in a table row, a pill and the inspector.
  """

  def node_tone(:ok), do: "ok"
  def node_tone(:converging), do: "warn"
  def node_tone(:issue), do: "bad"
  def node_tone(:blind), do: "idle"
  def node_tone(:silent), do: "faint"

  def path_tone(:installed), do: "ok"
  def path_tone(:drifted), do: "bad"
  def path_tone(:missing), do: "bad"
  def path_tone(:orphan), do: "warn"
  def path_tone(:pending), do: "warn"
  def path_tone(_status), do: "faint"

  def site_tone(:up), do: "ok"
  def site_tone(:converging), do: "warn"
  def site_tone(:no_tunnel), do: "warn"
  def site_tone(:no_report), do: "idle"
  def site_tone(:offline), do: "faint"

  def path_label(:installed), do: "Reconciled"
  def path_label(:drifted), do: "Drift"
  def path_label(:missing), do: "Not in FIB"
  def path_label(:orphan), do: "Orphan"
  def path_label(:pending), do: "Pending"
  def path_label(status), do: to_string(status)

  def site_label(:up), do: "Online"
  def site_label(:converging), do: "Converging"
  def site_label(:no_tunnel), do: "No tunnel"
  def site_label(:no_report), do: "No report"
  def site_label(:offline), do: "Offline"

  def kind_tone("rerouted"), do: "ok"
  def kind_tone("gated"), do: "warn"
  def kind_tone("link_down"), do: "bad"
  def kind_tone(_), do: "faint"

  def load_tone(ratio) when ratio >= 0.9, do: "bad"
  def load_tone(ratio) when ratio >= 0.6, do: "warn"
  def load_tone(_ratio), do: "faint"
end
