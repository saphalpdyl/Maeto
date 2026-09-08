defmodule MaetoPane.Fabric.Net do
  @moduledoc "Prefix arithmetic, so a segment address can be traced back to the node that owns it."

  import Bitwise

  @width 128

  def parse_addr(value) when is_binary(value) do
    case :inet.parse_address(String.to_charlist(value)) do
      {:ok, tuple} when tuple_size(tuple) == 8 -> {:ok, packed(tuple)}
      _ -> :error
    end
  end

  def parse_addr(_value), do: :error

  def parse_prefix(value) when is_binary(value) do
    with [addr, bits] <- String.split(value, "/", parts: 2),
         {:ok, packed} <- parse_addr(addr),
         {bits, ""} when bits in 0..@width <- Integer.parse(bits) do
      {:ok, {packed, bits}}
    else
      _ -> :error
    end
  end

  def parse_prefix(_value), do: :error

  def contains?({network, bits}, address) do
    shift = @width - bits

    bsr(address, shift) == bsr(network, shift)
  end

  defp packed(tuple) do
    tuple
    |> Tuple.to_list()
    |> Enum.reduce(0, fn hextet, acc -> acc * 65_536 + hextet end)
  end
end
