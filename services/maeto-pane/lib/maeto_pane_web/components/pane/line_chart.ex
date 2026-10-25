defmodule MaetoPaneWeb.Pane.LineChart do
  @moduledoc """
  A small time series chart in the reference style: value axis on the left, a
  bordered hatched plot, a solid and a dotted series, time labels underneath.
  Rendered as plain SVG, nothing on the client.
  """

  use MaetoPaneWeb, :html

  @width 240
  @height 72

  attr :id, :string, required: true
  attr :title, :string, required: true
  attr :unit, :string, required: true
  attr :solid, :list, required: true
  attr :dotted, :list, required: true
  attr :from, :any, required: true
  attr :floor, :float, default: 1.0

  def line_chart(assigns) do
    top = nice_max(assigns.solid ++ assigns.dotted, assigns.floor)

    assigns =
      assigns
      |> assign(:top, top)
      |> assign(:solid_paths, paths(assigns.solid, top))
      |> assign(:dotted_paths, paths(assigns.dotted, top))
      |> assign(:latest, latest(assigns.solid))
      |> assign(:width, @width)
      |> assign(:height, @height)
      |> assign(:times, bucket_times(assigns.from, length(assigns.solid)))

    ~H"""
    <div id={@id} class="space-y-2">
      <div class="flex items-center gap-2">
        <h4 class="text-[13px] font-medium">{@title}</h4>
        <span class="ml-auto rounded-[5px] bg-sunken px-1.5 font-mono text-[11.5px] tabular-nums">
          {format(@latest, @unit)}
        </span>
      </div>

      <div class="grid grid-cols-[44px_minmax(0,1fr)] gap-x-2">
        <div class="flex h-[88px] flex-col justify-between py-0.5 text-right text-[11px] text-muted">
          <span>{format(@top, @unit)}</span>
          <span>{format(@top / 2, @unit)}</span>
          <span>{format(0, @unit)}</span>
        </div>

        <div class="mt-plot relative h-[88px] rounded-md border border-line">
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
          <div class="absolute inset-0 flex">
            <div :for={{time, i} <- Enum.with_index(@times)} class="group relative flex-1">
              <div class="absolute inset-y-0 left-1/2 hidden w-px bg-line-strong group-hover:block" />

              <span
                :if={Enum.at(@solid, i)}
                class="absolute left-1/2 hidden size-1.5 -translate-x-1/2 translate-y-1/2 rounded-full bg-ok ring-2 ring-surface group-hover:block"
                style={"bottom: #{dot_bottom(Enum.at(@solid, i), @top)}%"}
              />
              <span
                :if={Enum.at(@dotted, i)}
                class="absolute left-1/2 hidden size-1.5 -translate-x-1/2 translate-y-1/2 rounded-full bg-steer ring-2 ring-surface group-hover:block"
                style={"bottom: #{dot_bottom(Enum.at(@dotted, i), @top)}%"}
              />

              <div class={[
                "absolute bottom-full z-10 mb-1.5 hidden whitespace-nowrap rounded-md border border-line bg-surface px-2 py-1 text-[11px] shadow-sm group-hover:block",
                if(i > length(@times) / 2, do: "right-0", else: "left-0")
              ]}>
                <div class="text-muted">
                  {Calendar.strftime(MaetoPaneWeb.Pane.UI.local(time), "%H:%M:%S")}
                </div>
                <div class="font-mono">
                  <span class="text-ok-ink">{format(Enum.at(@solid, i), @unit)}</span>
                  <span class="text-faint">/</span>
                  <span class="text-steer">{format(Enum.at(@dotted, i), @unit)}</span>
                </div>
              </div>
            </div>
          </div>
        </div>

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

  defp paths(values, top) do
    count = length(values)

    values
    |> Enum.with_index()
    |> Enum.chunk_by(fn {value, _index} -> value == nil end)
    |> Enum.reject(fn [{value, _index} | _] -> value == nil end)
    |> Enum.map(fn segment -> points(segment, count, top) end)
  end

  defp points(segment, count, top) do
    segment
    |> Enum.map(fn {value, index} ->
      x = x_for(index, count)
      y = @height - value / top * (@height - 4) - 2
      "#{Float.round(x, 2)},#{Float.round(y, 2)}"
    end)
    |> Enum.join(" ")
  end

  defp x_for(index, count), do: (index + 0.5) / count * @width

  defp bucket_times(_from, 0), do: []

  defp bucket_times(from, count) do
    Enum.map(0..(count - 1), fn index ->
      DateTime.add(from, index * MaetoPane.Pane.LinkStats.bucket_seconds(), :second)
    end)
  end

  defp dot_bottom(value, top) do
    from_bottom = 2 + value / top * (@height - 4)
    Float.round(from_bottom / @height * 100, 2)
  end

  defp nice_max(values, floor) do
    peak =
      values
      |> Enum.reject(&is_nil/1)
      |> Enum.max(fn -> 0 end)

    peak = max(peak, floor)
    magnitude = :math.pow(10, Float.floor(:math.log10(peak)))
    scaled = peak / magnitude

    step =
      cond do
        scaled <= 1 -> 1
        scaled <= 2 -> 2
        scaled <= 5 -> 5
        true -> 10
      end

    step * magnitude
  end

  defp latest(values) do
    values
    |> Enum.reverse()
    |> Enum.find(&(&1 != nil))
  end

  defp format(nil, _unit), do: "–"

  defp format(value, unit) do
    if value >= 10 or value == round(value) do
      "#{round(value)}#{unit}"
    else
      "#{:erlang.float_to_binary(value / 1, decimals: 1)}#{unit}"
    end
  end

  defp clock(from, 0), do: Calendar.strftime(MaetoPaneWeb.Pane.UI.local(from), "%H:%M")

  defp clock(from, 1) do
    from
    |> DateTime.add(div(MaetoPane.Pane.LinkStats.window_seconds(), 2), :second)
    |> MaetoPaneWeb.Pane.UI.local()
    |> Calendar.strftime("%H:%M")
  end
end
