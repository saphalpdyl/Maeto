defmodule MaetoPaneWeb.Pane.SitesTable do
  @moduledoc """
  Customer sites and whether their portal is reporting in.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :sites, :list, required: true
  attr :selection, :any, required: true

  def sites_table(assigns) do
    ~H"""
    <table class="w-full min-w-[720px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">Site</th>
          <th class="py-2.5">Tenant</th>
          <th class="py-2.5">Prefix</th>
          <th class="py-2.5">Attach</th>
          <th class="py-2.5">Portal</th>
          <th class="py-2.5 text-right">If</th>
          <th class="px-4 py-2.5 text-right">State</th>
        </tr>
      </thead>
      <tbody>
        <tr
          :for={site <- @sites}
          id={"site-#{site.portal_id}"}
          class={[
            "cursor-pointer border-b border-line last:border-0 hover:bg-sunken/70",
            selected?(@selection, :site, site.portal_id) && "bg-accent-soft"
          ]}
          phx-click="select"
          phx-value-kind="site"
          phx-value-id={site.portal_id}
        >
          <td class="px-4 py-2.5 font-medium text-accent">{site.cpe}</td>
          <td class="py-2.5 font-mono text-xs tabular-nums">{site.tenant}</td>
          <td class="py-2.5 font-mono text-xs text-muted">{site.prefix}</td>
          <td class="py-2.5 whitespace-nowrap">{site.attach_node}</td>
          <td class="py-2.5 font-mono text-xs text-faint">{site.portal_id}</td>
          <td class="py-2.5 text-right font-mono text-xs tabular-nums text-muted">
            {site.tunnel_if || site.if_id}
          </td>
          <td class="px-4 py-2.5 text-right">
            <.pill tone={site_tone(site.status)}>{site_label(site.status)}</.pill>
          </td>
        </tr>
        <tr :if={@sites == []}>
          <td colspan="7" class="px-4 py-10 text-center text-muted">
            No sites
          </td>
        </tr>
      </tbody>
    </table>
    """
  end
end
