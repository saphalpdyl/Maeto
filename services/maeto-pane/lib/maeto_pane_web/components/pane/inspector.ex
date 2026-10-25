defmodule MaetoPaneWeb.Pane.Inspector do
  @moduledoc """
  The side panel: details of whatever is selected, or the open issues when
  nothing is. Each kind of selection has its own panel module.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Inspector.Issues
  import MaetoPaneWeb.Pane.Inspector.LinkPanel
  import MaetoPaneWeb.Pane.Inspector.NodePanel
  import MaetoPaneWeb.Pane.Inspector.PathPanel
  import MaetoPaneWeb.Pane.Inspector.SitePanel

  attr :selected, :any, required: true
  attr :issues, :list, required: true
  attr :computed, :list, required: true
  attr :paths, :list, default: []
  attr :links, :list, default: []
  attr :nodes, :list, default: []
  attr :link_stats, :map, default: nil

  def panel(%{selected: nil} = assigns), do: ~H"<.issues_panel issues={@issues} />"
  def panel(%{selected: %{kind: :path}} = assigns), do: ~H"<.path_panel selected={@selected} />"
  def panel(%{selected: %{portal_id: _}} = assigns), do: ~H"<.site_panel selected={@selected} />"

  def panel(%{selected: %{kind: :node}} = assigns),
    do: ~H"<.node_panel selected={@selected} paths={@paths} links={@links} nodes={@nodes} />"

  def panel(%{selected: %{kind: :link}} = assigns),
    do:
      ~H"<.link_panel selected={@selected} computed={@computed} links={@links} stats={@link_stats} />"
end
