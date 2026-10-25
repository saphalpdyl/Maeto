defmodule MaetoPaneWeb.Pane.SyncChart do
  @moduledoc """
  Charts that move together. Every chart carries its series as data attributes;
  the ChartSync hook on the surrounding container reads the hovered bucket off
  whichever chart is under the cursor and shows that same bucket on all of them.
  Without the hook, each chart still shows its latest value.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.LineChart, only: [paths: 2, nice_max: 2, format: 2, latest: 1]

  attr :id, :string, required: true
  attr :title, :string, required: true
  attr :solid, :list, required: true
  attr :dotted, :list, required: true
  attr :unit, :string, required: true
  attr :from, :any, required: true
  attr :solid_label, :string, required: true
  attr :dotted_label, :string, required: true
  attr :height, :string, default: "h-[180px]"

  def wide(assigns) do
    top = nice_max(assigns.solid ++ assigns.dotted, 1.0)

    assigns =
      assigns
      |> assign(:top, top)
      |> assign(:solid_paths, paths(assigns.solid, top))
      |> assign(:dotted_paths, paths(assigns.dotted, top))
      |> assign(:latest_solid, latest(assigns.solid))
      |> assign(:latest_dotted, latest(assigns.dotted))

    ~H"""
    <div
      id={@id}
      data-chart
      data-series={Jason.encode!([@solid, @dotted])}
      data-unit={@unit}
      data-top={@top}
      data-from={DateTime.to_unix(@from, :millisecond)}
      data-bucket={MaetoPane.Pane.LinkStats.bucket_seconds() * 1000}
      class="space-y-3"
    >
      <div class="flex flex-wrap items-center gap-x-4 gap-y-1">
        <h2 class="text-[15px] font-medium">{@title}</h2>
        <span class="rounded-md bg-ok-soft px-2 py-px font-mono text-xs text-ok-ink">
          <span data-readout="0" data-latest={format(@latest_solid, @unit)}>
            {format(@latest_solid, @unit)}
          </span>
          in
        </span>
        <span class="rounded-md bg-steer-soft px-2 py-px font-mono text-xs text-steer-ink">
          <span data-readout="1" data-latest={format(@latest_dotted, @unit)}>
            {format(@latest_dotted, @unit)}
          </span>
          out
        </span>
        <span data-time class="font-mono text-xs text-muted" />

        <div class="ml-auto flex items-center gap-4 text-xs text-muted">
          <span class="flex items-center gap-1.5">
            <span class="h-0.5 w-3.5 rounded-full bg-ok" />{@solid_label}
          </span>
          <span class="flex items-center gap-1.5">
            <span class="flex w-3.5 justify-between">
              <span class="size-[3px] rounded-full bg-steer" />
              <span class="size-[3px] rounded-full bg-steer" />
              <span class="size-[3px] rounded-full bg-steer" />
            </span>
            {@dotted_label}
          </span>
        </div>
      </div>

      <div class="grid grid-cols-[64px_minmax(0,1fr)] gap-x-2">
        <div class={[
          "flex flex-col justify-between py-0.5 text-right text-[11px] text-muted",
          @height
        ]}>
          <span>{format(@top, @unit)}</span>
          <span>{format(@top / 2, @unit)}</span>
          <span>{format(0, @unit)}</span>
        </div>

        <.plot
          class={@height}
          solid_paths={@solid_paths}
          dotted_paths={@dotted_paths}
        />

        <div></div>
        <div class="flex justify-between pt-1.5 text-[11px] text-muted">
          <span>{clock(@from, 0)}</span>
          <span>{clock(@from, 1)}</span>
          <span class="flex items-center gap-1 text-ok-ink">
            Now <span class="size-1.5 rounded-full bg-ok" />
          </span>
        </div>
      </div>
    </div>
    """
  end

  attr :id, :string, required: true
  attr :rx, :list, required: true
  attr :tx, :list, required: true
  attr :unit, :string, required: true
  attr :from, :any, required: true

  def cell(assigns) do
    top = nice_max(assigns.rx ++ assigns.tx, 1.0)

    assigns =
      assigns
      |> assign(:top, top)
      |> assign(:rx_paths, paths(assigns.rx, top))
      |> assign(:tx_paths, paths(assigns.tx, top))
      |> assign(:latest_rx, latest(assigns.rx))
      |> assign(:latest_tx, latest(assigns.tx))

    ~H"""
    <div
      id={@id}
      data-chart
      data-series={Jason.encode!([@rx, @tx])}
      data-unit={@unit}
      data-top={@top}
      data-from={DateTime.to_unix(@from, :millisecond)}
      data-bucket={MaetoPane.Pane.LinkStats.bucket_seconds() * 1000}
      class="min-w-0 space-y-1"
    >
      <div class="flex items-baseline gap-2 font-mono text-[10.5px] tabular-nums">
        <span class="mr-auto truncate text-faint">{format(@top, @unit)}</span>
        <span data-readout="0" data-latest={format(@latest_rx, @unit)} class="text-ok-ink">
          {format(@latest_rx, @unit)}
        </span>
        <span data-readout="1" data-latest={format(@latest_tx, @unit)} class="text-steer-ink">
          {format(@latest_tx, @unit)}
        </span>
      </div>
      <.plot class="h-[56px]" solid_paths={@rx_paths} dotted_paths={@tx_paths} />
    </div>
    """
  end

  attr :class, :string, required: true
  attr :solid_paths, :list, required: true
  attr :dotted_paths, :list, required: true

  defp plot(assigns) do
    assigns =
      assigns
      |> assign(:width, MaetoPaneWeb.Pane.LineChart.width())
      |> assign(:height, MaetoPaneWeb.Pane.LineChart.height())

    ~H"""
    <div data-plot class={["mt-plot relative cursor-crosshair rounded-md border border-line", @class]}>
      <svg
        viewBox={"0 0 #{@width} #{@height}"}
        preserveAspectRatio="none"
        class="size-full"
        aria-hidden="true"
      >
        <polyline
          :for={points <- @solid_paths}
          points={points}
          fill="none"
          stroke="var(--mt-ok)"
          stroke-width="1.5"
          stroke-linejoin="round"
          vector-effect="non-scaling-stroke"
        />
        <polyline
          :for={points <- @dotted_paths}
          points={points}
          fill="none"
          stroke="var(--mt-steer)"
          stroke-width="1.75"
          stroke-linecap="round"
          stroke-dasharray="0 3.5"
          vector-effect="non-scaling-stroke"
        />
      </svg>
      <div data-cursor class="pointer-events-none absolute inset-y-0 hidden w-px bg-line-strong" />
      <span
        data-dot="0"
        class="pointer-events-none absolute hidden size-1.5 -translate-x-1/2 translate-y-1/2 rounded-full bg-ok ring-2 ring-surface"
      />
      <span
        data-dot="1"
        class="pointer-events-none absolute hidden size-1.5 -translate-x-1/2 translate-y-1/2 rounded-full bg-steer ring-2 ring-surface"
      />
    </div>
    """
  end

  defp clock(from, 0), do: Calendar.strftime(MaetoPaneWeb.Pane.UI.local(from), "%H:%M")

  defp clock(from, 1) do
    from
    |> DateTime.add(div(MaetoPane.Pane.LinkStats.window_seconds(), 2), :second)
    |> MaetoPaneWeb.Pane.UI.local()
    |> Calendar.strftime("%H:%M")
  end
end
