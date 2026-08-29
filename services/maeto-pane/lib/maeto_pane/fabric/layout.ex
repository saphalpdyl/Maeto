defmodule MaetoPane.Fabric.Layout do
  @moduledoc "Deterministic force-directed placement, scaled to fill the canvas."

  @width 760
  @height 520
  @margin 64
  @iterations 500
  @gravity 0.02
  @initial_temperature 0.12
  @max_stretch 1.7

  def width, do: @width
  def height, do: @height

  def positions([], _links), do: %{}

  def positions([single], _links), do: %{single => {@width / 2, @height / 2}}

  def positions(node_ids, links) do
    nodes = Enum.sort(node_ids)
    k = :math.sqrt(1.0 / length(nodes))
    pairs = Enum.filter(links, fn {a, b} -> a != b end)

    Enum.reduce(1..@iterations, seed(nodes), fn iteration, positions ->
      temperature = @initial_temperature * (1 - iteration / @iterations)

      positions
      |> displacement(pairs, k)
      |> advance(positions, temperature)
    end)
    |> orient()
    |> fit()
  end

  defp seed(nodes) do
    count = length(nodes)

    nodes
    |> Enum.with_index()
    |> Map.new(fn {id, index} ->
      angle = 2 * :math.pi() * index / count

      {id, {:math.cos(angle), :math.sin(angle)}}
    end)
  end

  defp displacement(positions, links, k) do
    zero = Map.new(positions, fn {id, _} -> {id, {0.0, 0.0}} end)

    positions
    |> Enum.reduce(zero, fn {v, pv}, acc ->
      Enum.reduce(positions, acc, fn
        {^v, _}, inner -> inner
        {_u, pu}, inner -> push(inner, v, pv, pu, fn d -> k * k / d end)
      end)
    end)
    |> attract(positions, links, k)
    |> gravitate(positions)
  end

  defp attract(acc, positions, links, k) do
    Enum.reduce(links, acc, fn {a, b}, inner ->
      with {:ok, pa} <- Map.fetch(positions, a), {:ok, pb} <- Map.fetch(positions, b) do
        inner
        |> push(a, pb, pa, fn d -> d * d / k end)
        |> push(b, pa, pb, fn d -> d * d / k end)
      else
        _ -> inner
      end
    end)
  end

  defp gravitate(acc, positions) do
    {cx, cy} = centroid(positions)

    Enum.reduce(positions, acc, fn {id, {x, y}}, inner ->
      {dx, dy} = Map.fetch!(inner, id)

      Map.put(inner, id, {dx + (cx - x) * @gravity, dy + (cy - y) * @gravity})
    end)
  end

  defp push(acc, id, {ax, ay}, {bx, by}, force) do
    dx = ax - bx
    dy = ay - by
    distance = max(:math.sqrt(dx * dx + dy * dy), 0.001)
    magnitude = force.(distance)
    {cx, cy} = Map.fetch!(acc, id)

    Map.put(acc, id, {cx + dx / distance * magnitude, cy + dy / distance * magnitude})
  end

  defp advance(displacement, positions, temperature) do
    Map.new(positions, fn {id, {x, y}} ->
      {dx, dy} = Map.fetch!(displacement, id)
      distance = max(:math.sqrt(dx * dx + dy * dy), 0.001)
      step = min(distance, max(temperature, 0.0))

      {id, {x + dx / distance * step, y + dy / distance * step}}
    end)
  end

  defp centroid(positions) do
    count = map_size(positions)

    {sx, sy} =
      Enum.reduce(positions, {0.0, 0.0}, fn {_, {x, y}}, {ax, ay} -> {ax + x, ay + y} end)

    {sx / count, sy / count}
  end

  defp orient(positions) do
    {cx, cy} = centroid(positions)

    {sxx, syy, sxy} =
      Enum.reduce(positions, {0.0, 0.0, 0.0}, fn {_, {x, y}}, {axx, ayy, axy} ->
        dx = x - cx
        dy = y - cy

        {axx + dx * dx, ayy + dy * dy, axy + dx * dy}
      end)

    angle = 0.5 * :math.atan2(2 * sxy, sxx - syy)
    target = if @width >= @height, do: 0.0, else: :math.pi() / 2
    rotation = target - angle

    cos = :math.cos(rotation)
    sin = :math.sin(rotation)

    Map.new(positions, fn {id, {x, y}} ->
      dx = x - cx
      dy = y - cy

      {id, {cx + dx * cos - dy * sin, cy + dx * sin + dy * cos}}
    end)
  end

  defp fit(positions) do
    xs = Enum.map(positions, fn {_, {x, _}} -> x end)
    ys = Enum.map(positions, fn {_, {_, y}} -> y end)

    {min_x, max_x} = Enum.min_max(xs)
    {min_y, max_y} = Enum.min_max(ys)

    span_x = max(max_x - min_x, 1.0e-6)
    span_y = max(max_y - min_y, 1.0e-6)

    fit_x = (@width - 2 * @margin) / span_x
    fit_y = (@height - 2 * @margin) / span_y
    uniform = min(fit_x, fit_y)

    scale_x = min(fit_x, uniform * @max_stretch)
    scale_y = min(fit_y, uniform * @max_stretch)

    offset_x = (@width - span_x * scale_x) / 2
    offset_y = (@height - span_y * scale_y) / 2

    Map.new(positions, fn {id, {x, y}} ->
      {id, {offset_x + (x - min_x) * scale_x, offset_y + (y - min_y) * scale_y}}
    end)
  end
end
