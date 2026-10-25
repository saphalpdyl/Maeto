defmodule MaetoPane.Pane.LinkStats do
  @moduledoc """
  Latency, jitter and loss for one adjacency, per direction, bucketed over a
  recent window of raw probe samples.
  """

  @window_seconds 30 * 60
  @bucket_seconds 30

  @query """
  SELECT telemetry_key, time, sequence, latency_ms
  FROM link_samples
  WHERE telemetry_key = ANY($1)
    AND time >= $2
  ORDER BY telemetry_key, time
  """

  def window_seconds, do: @window_seconds
  def bucket_seconds, do: @bucket_seconds

  def for_link(link, now \\ DateTime.utc_now()) do
    from = DateTime.add(now, -@window_seconds, :second)
    {forward_keys, reverse_keys} = keys(link)

    case query(forward_keys ++ reverse_keys, from) do
      {:ok, rows} ->
        by_key = Enum.group_by(rows, fn [key | _] -> key end)

        %{
          from: from,
          to: now,
          forward: direction(by_key, forward_keys, from),
          reverse: direction(by_key, reverse_keys, from)
        }

      {:error, _reason} ->
        nil
    end
  end

  # a member "A:eth2-B:eth2" is probed by A as itself and by B as "B:eth2-A:eth2"
  def keys(link) do
    members = Enum.map(link.edges, & &1["id"])
    all = Enum.flat_map(members, fn id -> [id, reverse_key(id)] end) |> Enum.uniq()

    forward = Enum.filter(all, &String.starts_with?(&1, link.a <> ":"))
    reverse = Enum.filter(all, &String.starts_with?(&1, link.b <> ":"))

    {forward, reverse}
  end

  defp reverse_key(id) do
    case String.split(id, "-") do
      [local, remote] -> remote <> "-" <> local
      _ -> id
    end
  end

  defp query([], _from), do: {:ok, []}

  defp query(keys, from) do
    case Postgrex.query(MaetoPane.DB, @query, [keys, from]) do
      {:ok, result} -> {:ok, result.rows}
      {:error, error} -> {:error, error}
    end
  catch
    :exit, reason -> {:error, reason}
  end

  defp direction(by_key, keys, from) do
    empty = List.duplicate(%{latencies: [], jitters: [], received: 0, lost: 0}, bucket_count())

    buckets =
      Enum.reduce(keys, empty, fn key, acc ->
        add_key(acc, Map.get(by_key, key, []), from)
      end)

    %{
      latency: Enum.map(buckets, &average(&1.latencies)),
      jitter: Enum.map(buckets, &average(&1.jitters)),
      loss: Enum.map(buckets, &loss/1)
    }
  end

  defp add_key(buckets, [], _from), do: buckets

  defp add_key(buckets, rows, from) do
    step = sequence_step(rows)

    {buckets, _previous} =
      Enum.reduce(rows, {buckets, nil}, fn [_key, time, sequence, latency], {acc, previous} ->
        index = bucket_index(time, from)

        if index == nil do
          {acc, [sequence, latency]}
        else
          bucket = Enum.at(acc, index)
          bucket = add_sample(bucket, latency, sequence, previous, step)
          {List.replace_at(acc, index, bucket), [sequence, latency]}
        end
      end)

    buckets
  end

  defp add_sample(bucket, latency, _sequence, nil, _step) do
    %{bucket | latencies: [latency | bucket.latencies], received: bucket.received + 1}
  end

  defp add_sample(bucket, latency, sequence, [previous_sequence, previous_latency], step) do
    gap = sequence - previous_sequence

    missed =
      if gap > step do
        div(gap, step) - 1
      else
        0
      end

    %{
      bucket
      | latencies: [latency | bucket.latencies],
        jitters: [abs(latency - previous_latency) | bucket.jitters],
        received: bucket.received + 1,
        lost: bucket.lost + missed
    }
  end

  # a sender may share one counter across sessions, so the smallest forward
  # step seen is what "no loss" looks like for this key
  defp sequence_step(rows) do
    steps =
      rows
      |> Enum.map(fn [_key, _time, sequence, _latency] -> sequence end)
      |> Enum.chunk_every(2, 1, :discard)
      |> Enum.map(fn [a, b] -> b - a end)
      |> Enum.filter(&(&1 > 0))

    case steps do
      [] -> 1
      _ -> Enum.min(steps)
    end
  end

  defp bucket_index(time, from) do
    seconds = DateTime.diff(time, from, :second)
    index = div(seconds, @bucket_seconds)

    if index >= 0 and index < bucket_count() do
      index
    else
      nil
    end
  end

  defp bucket_count, do: div(@window_seconds, @bucket_seconds)

  defp average([]), do: nil
  defp average(values), do: Enum.sum(values) / length(values)

  defp loss(%{received: 0, lost: 0}), do: nil
  defp loss(%{received: received, lost: lost}), do: lost / (received + lost) * 100
end
