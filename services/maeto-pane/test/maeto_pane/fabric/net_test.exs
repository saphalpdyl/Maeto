defmodule MaetoPane.Fabric.NetTest do
  use ExUnit.Case, async: true

  alias MaetoPane.Fabric.Net

  describe "parse_prefix/1" do
    test "accepts a locator" do
      assert {:ok, {_packed, 48}} = Net.parse_prefix("fc00:0:1::/48")
    end

    test "rejects junk, a bare address and an impossible length" do
      assert Net.parse_prefix("not-a-prefix") == :error
      assert Net.parse_prefix("fc00:0:1::") == :error
      assert Net.parse_prefix("fc00:0:1::/129") == :error
      assert Net.parse_prefix(nil) == :error
    end
  end

  describe "contains?/2" do
    setup do
      {:ok, prefix} = Net.parse_prefix("fc00:0:1::/48")

      %{prefix: prefix}
    end

    test "a dt46 sid sits inside the locator that minted it", %{prefix: prefix} do
      {:ok, sid} = Net.parse_addr("fc00:0:1:ff8c::")

      assert Net.contains?(prefix, sid)
    end

    test "the locator base address counts as inside", %{prefix: prefix} do
      {:ok, base} = Net.parse_addr("fc00:0:1::")

      assert Net.contains?(prefix, base)
    end

    test "a neighbour's locator does not", %{prefix: prefix} do
      {:ok, sid} = Net.parse_addr("fc00:0:2:ff8c::")

      refute Net.contains?(prefix, sid)
    end
  end
end
