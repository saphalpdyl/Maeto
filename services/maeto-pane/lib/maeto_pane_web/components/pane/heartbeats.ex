defmodule MaetoPaneWeb.Pane.Heartbeats do
  @moduledoc "Tick bars built from each node's topology reports."

  use MaetoPaneWeb, :html

  attr :nodes, :list, required: true
  attr :selected, :string, default: nil

  def heartbeats(assigns) do
    ~H"""
    <div class="grid grid-cols-1 gap-x-6 gap-y-4 lg:grid-cols-2">
      <div
        :for={node <- Enum.sort_by(@nodes, &(&1.name || &1.id))}
        id={"heartbeat-#{node.id}"}
        phx-click="select"
        phx-value-kind="node"
        phx-value-id={node.id}
        class={[
          "min-w-0 cursor-pointer rounded-md p-1.5 transition-colors hover:bg-sunken",
          @selected == node.id && "bg-steer-soft ring-1 ring-steer-ring ring-inset"
        ]}
      >
        <div class="mb-1.5 flex items-center justify-between gap-2">
          <span class="truncate text-[13px]">{node.name || node.id}</span>
          <span class={["font-mono text-xs", tone_text(List.last(node.heartbeat))]}>
            {label(List.last(node.heartbeat))}
          </span>
        </div>
        <.heartbeat_bar bars={node.heartbeat} />
      </div>
    </div>
    """
  end

  attr :bars, :list, required: true

  def heartbeat_bar(assigns) do
    ~H"""
    <div class="flex h-7 items-stretch gap-[3px] rounded-md border border-line bg-surface px-2 py-1.5">
      <span :for={bar <- @bars} class={["flex-1 rounded-full", tone_bg(bar)]} />
    </div>
    """
  end

  def label(:ready), do: "ready"
  def label(:not_ready), do: "not ready"
  def label(:missing), do: "silent"
  def label(_unknown), do: "-"

  def tone_text(:ready), do: "text-ok-ink"
  def tone_text(:not_ready), do: "text-warn-ink"
  def tone_text(:missing), do: "text-bad-ink"
  def tone_text(_unknown), do: "text-faint"

  defp tone_bg(:ready), do: "bg-ok"
  defp tone_bg(:not_ready), do: "bg-warn"
  defp tone_bg(:missing), do: "bg-bad"
  defp tone_bg(_unknown), do: "bg-line"
end
