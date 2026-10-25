defmodule MaetoPane.Pane.IfaceStats do
  @moduledoc """
  Per-interface rates for one PoP, from the raw cumulative counters the agent
  publishes: bits, packets and drops per second, per direction, bucketed over
  the same window as the link charts.

  A counter that goes down means the interface was recreated or the node
  restarted; that interval is skipped rather than read as a huge negative rate.
  """

  alias MaetoPane.Pane.LinkStats

  @stats [:rx_bytes, :tx_bytes, :rx_packets, :tx_packets, :rx_dropped, :tx_dropped]

  @query """
  SELECT iface, time, rx_bytes, tx_bytes, rx_packets, tx_packets, rx_dropped, tx_dropped
  FROM iface_counters
  WHERE node_id = $1
    AND time >= $2
  ORDER BY iface, time
  """

  @fabric_query """
  SELECT node_id || ':' || iface, time, rx_bytes, tx_bytes, rx_packets, tx_packets, rx_dropped, tx_dropped
  FROM iface_counters
  WHERE node_id || ':' || iface = ANY($1)
    AND time >= $2
  ORDER BY node_id, iface, time
  """

  def stats, do: @stats

  @doc """
  Traffic entering and leaving the SR domain: every PoP's access interface,
  where tenant traffic from the CPEs arrives and leaves. `access` is a list of
  `{node_id, iface}`; rx is what the fabric took in, tx what it handed out.
  """
  def fabric(access, now \\ DateTime.utc_now()) do
    from = DateTime.add(now, -LinkStats.window_seconds(), :second)
    fetch_from = DateTime.add(from, -LinkStats.bucket_seconds(), :second)
    keys = Enum.map(access, fn {node_id, iface} -> "#{node_id}:#{iface}" end)

    case run(@fabric_query, [keys, fetch_from]) do
      {:ok, rows} ->
        series =
          rows
          |> Enum.group_by(fn [key | _] -> key end)
          |> Enum.map(fn {_key, key_rows} -> device_series(key_rows, from) end)

        %{
          from: from,
          to: now,
          rx_bits: sum_series(Enum.map(series, & &1.rx_bytes)),
          tx_bits: sum_series(Enum.map(series, & &1.tx_bytes))
        }

      {:error, _reason} ->
        nil
    end
  end

  def for_node(node_id, core_ifaces, now \\ DateTime.utc_now()) do
    from = DateTime.add(now, -LinkStats.window_seconds(), :second)
    fetch_from = DateTime.add(from, -LinkStats.bucket_seconds(), :second)

    case query(node_id, fetch_from) do
      {:ok, rows} ->
        devices =
          rows
          |> Enum.group_by(fn [iface | _] -> iface end)
          |> Enum.map(fn {iface, iface_rows} -> {iface, device_series(iface_rows, from)} end)
          |> Enum.sort_by(fn {iface, _series} -> device_order(iface) end)

        %{from: from, to: now, devices: devices, total: total(devices, core_ifaces)}

      {:error, _reason} ->
        nil
    end
  end

  defp query(node_id, from), do: run(@query, [node_id, from])

  defp run(sql, params) do
    case Postgrex.query(MaetoPane.DB, sql, params) do
      {:ok, result} -> {:ok, result.rows}
      {:error, error} -> {:error, error}
    end
  catch
    :exit, reason -> {:error, reason}
  end

  # one series per counter: the rate in each bucket is the counter change over
  # the time it took, summed across the samples that ended in that bucket
  defp device_series(rows, from) do
    empty = List.duplicate(%{}, bucket_count())

    sums =
      rows
      |> Enum.chunk_every(2, 1, :discard)
      |> Enum.reduce(empty, fn [previous, current], buckets ->
        add_interval(buckets, previous, current, from)
      end)

    Map.new(@stats, fn stat ->
      {stat, Enum.map(sums, &rate(&1, stat))}
    end)
  end

  defp add_interval(buckets, previous, current, from) do
    [_iface, previous_time | previous_values] = previous
    [_iface, time | values] = current
    index = bucket_index(time, from)
    seconds = DateTime.diff(time, previous_time, :millisecond) / 1000

    if index == nil or seconds <= 0 do
      buckets
    else
      bucket = Enum.at(buckets, index)

      bucket =
        @stats
        |> Enum.zip(Enum.zip(previous_values, values))
        |> Enum.reduce(bucket, fn {stat, {before, now}}, acc ->
          delta = now - before

          if delta < 0 do
            acc
          else
            {sum, elapsed} = Map.get(acc, stat, {0, 0.0})
            Map.put(acc, stat, {sum + delta, elapsed + seconds})
          end
        end)

      List.replace_at(buckets, index, bucket)
    end
  end

  defp rate(bucket, stat) do
    case Map.get(bucket, stat) do
      nil -> nil
      {_sum, elapsed} when elapsed <= 0 -> nil
      {sum, elapsed} -> sum / elapsed * multiplier(stat)
    end
  end

  defp multiplier(stat) when stat in [:rx_bytes, :tx_bytes], do: 8
  defp multiplier(_stat), do: 1

  # tenant traffic crosses an access link or tunnel and then a core link, so
  # only the core links count towards what the PoP moves across the fabric
  defp total(devices, core_ifaces) do
    physical = for {iface, series} <- devices, iface in core_ifaces, do: series

    %{
      rx_bits: sum_series(Enum.map(physical, & &1.rx_bytes)),
      tx_bits: sum_series(Enum.map(physical, & &1.tx_bytes))
    }
  end

  defp sum_series([]), do: List.duplicate(nil, bucket_count())

  defp sum_series(series) do
    series
    |> Enum.zip()
    |> Enum.map(fn column ->
      values = column |> Tuple.to_list() |> Enum.reject(&is_nil/1)

      case values do
        [] -> nil
        _ -> Enum.sum(values)
      end
    end)
  end

  defp device_order("eth" <> number), do: {0, String.pad_leading(number, 4, "0")}
  defp device_order("maeto-tun-" <> rest), do: {1, rest}
  defp device_order("maeto-vrf-" <> rest), do: {2, rest}
  defp device_order(other), do: {3, other}

  defp bucket_index(time, from) do
    seconds = DateTime.diff(time, from, :second)
    index = div(seconds, LinkStats.bucket_seconds())

    if seconds >= 0 and index < bucket_count() do
      index
    else
      nil
    end
  end

  defp bucket_count, do: div(LinkStats.window_seconds(), LinkStats.bucket_seconds())
end
