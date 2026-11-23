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
        <div class="mb-2 flex items-center justify-between gap-2">
          <span class="truncate text-[13px]">{node.name || node.id}</span>
          <span class={["font-mono text-xs", tone_text(List.last(node.heartbeat))]}>
            {label(List.last(node.heartbeat))}
          </span>
        </div>
        <.heartbeat_bar id={"heartbeat-bar-#{node.id}"} bars={node.heartbeat} />
        <.heartbeat_axis bars={node.heartbeat} />
      </div>
    </div>
    """
  end

  attr :id, :string, required: true
  attr :bars, :list, required: true

  def heartbeat_bar(assigns) do
    ~H"""
    <div class="h-7 rounded-md border border-line bg-surface px-2 py-1.5">
      <div
        id={@id}
        phx-hook=".TickPitch"
        class="flex h-full [mask-image:repeating-linear-gradient(to_right,black_0_var(--hb-tick,3px),transparent_var(--hb-tick,3px)_var(--hb-pitch,5px))]"
      >
        <span :for={bar <- @bars} class={["flex-1", tone_bg(bar)]} />
      </div>
    </div>
    <script :type={Phoenix.LiveView.ColocatedHook} name=".TickPitch">
      export default {
        mounted() {
          this.apply = () => {
            const dpr = window.devicePixelRatio || 1
            const px = n => `${Math.max(1, Math.round(n * dpr)) / dpr}px`
            this.el.style.setProperty("--hb-tick", px(3))
            this.el.style.setProperty("--hb-pitch", px(5))
          }
          this.apply()
          window.addEventListener("resize", this.apply)
        },
        updated() {
          this.apply()
        },
        destroyed() {
          window.removeEventListener("resize", this.apply)
        }
      }
    </script>
    """
  end

  attr :bars, :list, required: true

  def heartbeat_axis(assigns) do
    ~H"""
    <div class="mt-1.5 flex items-center gap-3 font-mono text-[11px] text-faint">
      <span>{span_label(length(@bars))}</span>
      <span class="h-px flex-1 bg-line" />
      <span>{ready_share(@bars)}</span>
      <span class="h-px flex-1 bg-line" />
      <span>now</span>
    </div>
    """
  end

  defp span_label(count) do
    "#{div(count * MaetoPane.Pane.Heartbeat.width_ms(), 60_000)}m ago"
  end

  defp ready_share(bars) do
    known = Enum.reject(bars, &(&1 == :unknown))

    case known do
      [] -> "-"
      _ -> "#{Float.round(Enum.count(known, &(&1 == :ready)) * 100 / length(known), 1)}% ready"
    end
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
