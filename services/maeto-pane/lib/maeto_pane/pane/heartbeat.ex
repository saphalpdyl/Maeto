defmodule MaetoPane.Pane.Heartbeat do
  @moduledoc """
  A node's topology reports as fixed-width time buckets, newest last: ready
  when every report in the bucket said ready, not ready when any said
  otherwise, missing when none arrived, and unknown before the pane was
  watching.
  """

  @buckets 75
  @width_ms 20_000

  def buckets, do: @buckets
  def width_ms, do: @width_ms

  def bars(snapshot, node_key, now \\ System.system_time(:millisecond)) do
    beats = Map.get(snapshot.heartbeats || %{}, node_key, [])
    started_at = snapshot.started_at || now

    last_complete = div(now, @width_ms) * @width_ms

    for i <- (@buckets - 1)..0//-1 do
      to = last_complete - i * @width_ms
      from = to - @width_ms
      in_bucket = for {at, ready} <- beats, at > from and at <= to, do: ready

      cond do
        in_bucket != [] and Enum.all?(in_bucket) -> :ready
        in_bucket != [] -> :not_ready
        to <= started_at -> :unknown
        true -> :missing
      end
    end
  end
end
