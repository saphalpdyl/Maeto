defmodule MaetoPane.Pane.Costs do
  @moduledoc """
  Reads the controller's normalized cost dimensions back into units a person
  knows, and adds them up the way CSPF does for each class. Each dimension is
  1.0 at its reference; a class's edge cost is epsilon plus its weighted sum.

  The controller publishes its references and epsilon as cost_model; the
  defaults here only cover a controller that predates it.
  """

  @defaults %{
    edge_epsilon: 1.0,
    delay_ms: 10,
    jitter_ms: 5,
    loss: 0.01,
    utilization: 0.8
  }

  @dimensions [
    {"LATENCY", "Latency"},
    {"JITTER", "Jitter"},
    {"LOSS", "Loss"},
    {"STATIC_UTILIZATION", "Static utilization"}
  ]

  def model(snapshot) do
    published = get_in(Map.get(snapshot, :control) || %{}, ["cost_model"]) || %{}

    %{
      edge_epsilon: published["edge_epsilon"] || @defaults.edge_epsilon,
      delay_ms: published["delay_ref_ms"] || @defaults.delay_ms,
      jitter_ms: published["jitter_ref_ms"] || @defaults.jitter_ms,
      loss: published["loss_ref"] || @defaults.loss,
      utilization: published["utilization_ref"] || @defaults.utilization
    }
  end

  def default_model, do: @defaults

  @doc "The dimensions an edge carries, known ones first in a fixed order."
  def breakdown(costs, model \\ @defaults)
  def breakdown(nil, _model), do: []

  def breakdown(costs, model) when is_map(costs) do
    known =
      for {key, label} <- @dimensions, Map.has_key?(costs, key) do
        %{key: key, label: label, value: costs[key], reads: reads(key, costs[key], model)}
      end

    known_keys = Enum.map(@dimensions, &elem(&1, 0))

    rest =
      for {key, value} <- Enum.sort(costs), key not in known_keys do
        %{key: key, label: humanize(key), value: value, reads: nil}
      end

    known ++ rest
  end

  @doc """
  What one edge costs a class: epsilon plus the class's weighted dimensions.
  nil when a weighted dimension is missing, which the controller does for an
  edge it has no measurement for or one that is saturated.
  """
  def class_total(nil, _objective, _model), do: nil

  # a class whose weights were not published cannot be priced; epsilon alone
  # would read as a real, suspiciously round cost
  def class_total(_costs, objective, _model) when objective in [nil, %{}], do: nil

  def class_total(costs, objective, model) do
    Enum.reduce_while(objective || %{}, model.edge_epsilon, fn {dim, weight}, total ->
      case costs[dim] do
        value when is_number(value) -> {:cont, total + weight * value}
        _missing -> {:halt, nil}
      end
    end)
  end

  def delay_ms(cost, model \\ @defaults)
  def delay_ms(nil, _model), do: nil
  def delay_ms(cost, model), do: cost * model.delay_ms

  # jitter is a variance normalized by the squared reference
  def jitter_ms(cost, model \\ @defaults)
  def jitter_ms(nil, _model), do: nil
  def jitter_ms(cost, _model) when cost < 0, do: nil
  def jitter_ms(cost, model), do: :math.sqrt(cost) * model.jitter_ms

  # loss is -ln(1-p) normalized by -ln(1-p_ref), so p = 1 - (1-p_ref)^cost
  def loss(cost, model \\ @defaults)
  def loss(nil, _model), do: nil
  def loss(cost, model), do: 1 - :math.pow(1 - model.loss, cost)

  defp reads(_key, value, _model) when not is_number(value), do: nil
  defp reads("LATENCY", value, model), do: "#{fixed(delay_ms(value, model))} ms"
  defp reads("JITTER", value, model), do: "σ #{fixed(jitter_ms(value, model))} ms"
  defp reads("LOSS", value, model), do: "#{fixed(loss(value, model) * 100, 2)}%"
  defp reads(_key, _value, _model), do: nil

  defp fixed(nil, _decimals), do: "–"
  defp fixed(value, decimals), do: :erlang.float_to_binary(value * 1.0, decimals: decimals)
  defp fixed(value), do: fixed(value, 1)

  defp humanize(key),
    do: key |> String.downcase() |> String.replace("_", " ") |> String.capitalize()
end
