defmodule MaetoPaneWeb.Pane.RegistryPanel do
  @moduledoc """
  The controller's SID allocations and the intents it holds per node.
  """

  use MaetoPaneWeb, :html

  attr :registry, :map, required: true

  def registry_panel(assigns) do
    ~H"""
    <div :if={!@registry.present?} class="px-4 py-10 text-center text-[13px] text-muted">
      The controller has not published its registry yet.
    </div>

    <div :if={@registry.present?}>
      <table class="w-full min-w-[520px] border-collapse text-left text-[13px]">
        <thead class="border-b border-line text-xs font-medium text-muted">
          <tr>
            <th class="px-4 py-2.5">Tenant</th>
            <th class="py-2.5">Behaviour</th>
            <th class="py-2.5">Locator</th>
            <th class="px-4 py-2.5">SID</th>
          </tr>
        </thead>
        <tbody>
          <tr
            :for={allocation <- @registry.tenants}
            class="border-b border-line hover:bg-sunken/70"
          >
            <td class="px-4 py-2 font-mono font-medium tabular-nums">{allocation.tenant}</td>
            <td class="py-2 font-mono text-xs uppercase text-muted">{allocation.type}</td>
            <td class="py-2 font-mono text-xs text-muted">{allocation.locator}</td>
            <td class="px-4 py-2 font-mono text-xs">{allocation.sid}</td>
          </tr>
          <tr :if={@registry.tenants == []}>
            <td colspan="4" class="px-4 py-6 text-center text-muted">Nothing allocated</td>
          </tr>
        </tbody>
      </table>

      <div class="border-t border-line bg-sunken/60 px-4 py-2">
        <h3 class="text-xs font-semibold">Held intents</h3>
      </div>
      <table class="w-full min-w-[520px] border-collapse text-left text-[13px]">
        <thead class="border-b border-line text-xs font-medium text-muted">
          <tr>
            <th class="px-4 py-2.5">Node</th>
            <th class="py-2.5">Gen</th>
            <th class="py-2.5">Tenant</th>
            <th class="py-2.5">Sites</th>
            <th class="px-4 py-2.5">End.DT46</th>
          </tr>
        </thead>
        <tbody>
          <%= for node <- @registry.nodes, tenant <- node.tenants do %>
            <tr class={["border-b border-line last:border-0", tenant.drifted? && "bg-bad-soft"]}>
              <td class="px-4 py-2 font-mono font-medium">{node.id}</td>
              <td class="py-2 font-mono text-xs tabular-nums text-muted">{node.generation}</td>
              <td class="py-2 font-mono text-xs tabular-nums">{tenant.id}</td>
              <td class="py-2 tabular-nums text-muted">{tenant.portals}</td>
              <td class="px-4 py-2 font-mono text-xs">
                {tenant.registry_sid}
                <span :if={tenant.drifted?} class="text-bad">
                  (published {tenant.published_sid || "nothing"})
                </span>
              </td>
            </tr>
          <% end %>
          <tr :if={@registry.nodes == []}>
            <td colspan="5" class="px-4 py-6 text-center text-muted">No intents held</td>
          </tr>
        </tbody>
      </table>
    </div>
    """
  end
end
