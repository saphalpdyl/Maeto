defmodule MaetoPaneWeb.Pane.Inspector.SitePanel do
  @moduledoc """
  A selected customer site: its service and where it attaches.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :selected, :map, required: true

  def site_panel(assigns) do
    ~H"""
    <.head
      title={@selected.cpe}
      subtitle={@selected.identity}
      tone={site_tone(@selected.status)}
      state={site_label(@selected.status)}
    />

    <.group title="Service">
      <.row label="Tenant" value={@selected.tenant} />
      <.row label="Allocation" value={@selected.allocation} />
      <.row label="Site prefix" value={@selected.prefix} />
      <.row label="VRF table" value={@selected.vrf_table} />
    </.group>

    <.group title="Attachment">
      <.row label="Attach PoP" value={@selected.attach} />
      <.row label="PoP name" value={@selected.attach_node} />
      <.row label="Portal id" value={@selected.portal_id} />
      <.row label="Provisioned if" value={@selected.if_id} />
      <.row label="Tunnel if" value={@selected.tunnel_if} />
      <.row label="Reported at" value={clock(@selected.reported_at)} />
    </.group>
    """
  end
end
