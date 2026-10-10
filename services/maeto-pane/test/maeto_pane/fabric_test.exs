defmodule MaetoPane.FabricTest do
  use ExUnit.Case, async: true

  alias MaetoPane.Fabric

  test "intent keys drop their lane" do
    assert Fabric.node_key(:intents, "pop.A.intent") == {:ok, "pop.A"}
    assert Fabric.node_key(:intents, "cpe.aa400723fee3.intent") == {:ok, "cpe.aa400723fee3"}
  end

  test "state keys keep only the dataplane lane" do
    assert Fabric.node_key(:states, "pop.A.dataplane") == {:ok, "pop.A"}
    assert Fabric.node_key(:states, "pop.A.topology") == :other_lane
  end

  test "keys without a lane are ignored" do
    assert Fabric.node_key(:states, "pop.A") == :other_lane
  end
end
