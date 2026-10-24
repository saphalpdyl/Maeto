defmodule MaetoPaneWeb.Pane.PathsTable do
  @moduledoc """
  Segment lists per ingress PE: desired against what the node's FIB holds.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :paths, :list, required: true
  attr :selection, :any, required: true

  def paths_table(assigns) do
    ~H"""
    <table class="w-full min-w-[940px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs text-faint">
        <tr>
          <th class="px-4 py-2 font-normal">Towards</th>
          <th class="py-2 font-normal">Tenant</th>
          <th class="py-2 font-normal">Class</th>
          <th class="py-2 font-normal">Destination</th>
          <th class="py-2 font-normal">Segment list</th>
          <th class="py-2 text-right font-normal">Cost</th>
          <th class="px-4 py-2 font-normal">Dataplane</th>
        </tr>
      </thead>
      <tbody :for={{pe, rows} <- Enum.sort(Enum.group_by(@paths, & &1.pe))}>
        <tr class="border-b border-line bg-sunken/50">
          <td colspan="7" class="px-4 py-1.5 text-xs text-muted">
            <span class="mr-2 rounded border border-line bg-surface px-1.5 font-mono font-medium text-ink">
              {pe}
            </span>
            ingress · {length(rows)} {if length(rows) == 1, do: "list", else: "lists"}
          </td>
        </tr>
        <tr
          :for={row <- rows}
          id={"path-#{:erlang.phash2(row.id)}"}
          class={[
            "cursor-pointer border-b border-line/60 transition-colors hover:bg-sunken/50",
            selected?(@selection, :path, row.id) &&
              "bg-steer-soft shadow-[inset_2px_0_0_var(--mt-steer)]"
          ]}
          phx-click="select"
          phx-value-kind="path"
          phx-value-id={row.id}
        >
          <td class="px-4 py-3 whitespace-nowrap">
            <span class="font-medium text-link">{row.dest_name || row.dest || "?"}</span>
          </td>
          <td class="py-3 pr-4 whitespace-nowrap">
            <span :if={row.site}>{row.site.cpe}</span>
            <span class="ml-1 font-mono text-[11px] text-faint tabular-nums">{row.tenant}</span>
          </td>
          <td class="py-3 pr-4 whitespace-nowrap text-muted">{row.class}</td>
          <td class="max-w-[220px] truncate py-3 pr-3 font-mono text-xs text-muted">{row.prefix}</td>
          <td class="py-3 pr-3"><.chain hops={row.hops} /></td>
          <td class="py-3 text-right font-mono tabular-nums">{num(row.cost)}</td>
          <td class="px-4 py-3">
            <.pill tone={path_tone(row.status)}>{path_label(row.status)}</.pill>
          </td>
        </tr>
      </tbody>
      <tbody :if={@paths == []}>
        <tr>
          <td colspan="7" class="px-4 py-10 text-center text-muted">
            No segment lists
          </td>
        </tr>
      </tbody>
    </table>
    """
  end
end
