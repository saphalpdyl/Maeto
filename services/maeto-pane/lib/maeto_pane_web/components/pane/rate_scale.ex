defmodule MaetoPaneWeb.Pane.RateScale do
  @moduledoc """
  Picks a readable unit for a set of rates and scales them into it, so a chart
  reads "12 Mb/s" rather than "12000000 b/s".
  """

  def bits(series_list),
    do: scale(series_list, [{1.0e9, "Gb/s"}, {1.0e6, "Mb/s"}, {1.0e3, "kb/s"}], "b/s")

  def packets(series_list), do: scale(series_list, [{1.0e6, "Mp/s"}, {1.0e3, "kp/s"}], "p/s")
  def events(series_list), do: scale(series_list, [{1.0e3, "k/s"}], "/s")

  defp scale(series_list, steps, base_unit) do
    peak =
      series_list
      |> List.flatten()
      |> Enum.reject(&is_nil/1)
      |> Enum.max(fn -> 0 end)

    {divisor, unit} =
      Enum.find(steps, {1.0, base_unit}, fn {threshold, _unit} -> peak >= threshold end)

    scaled =
      Enum.map(series_list, fn series ->
        Enum.map(series, fn
          nil -> nil
          value -> value / divisor
        end)
      end)

    {scaled, unit}
  end
end
