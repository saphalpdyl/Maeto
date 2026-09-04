package controlplane

import (
	"maps"
	"slices"
	"testing"
)

var whatIf = struct {
	Objective Objective
	Overrides map[[2]NodeID]map[CostDimension]float64
	Pairs     [][2]NodeID
}{
	Objective: MinDimension(COSTDIM_LATENCY),

	Overrides: map[[2]NodeID]map[CostDimension]float64{
		{"F", "H"}: {COSTDIM_LATENCY: 20},
	},

	Pairs: nil,
}

func TestWhatIf(t *testing.T) {
	g := loadTestGraph(t)

	costGraph, err := NewTestCostGraph(g)
	if err != nil {
		t.Fatalf("build cost graph: %v", err)
	}

	before := costGraph.Costs()
	after := applyOverrides(g, before, whatIf.Overrides)

	objective := whatIf.Objective
	if objective == nil {
		objective = MinIGPMetric
	}

	pairs := whatIf.Pairs
	if len(pairs) == 0 {
		pairs = allPairs(g)
	}

	changed := 0
	t.Logf("%-10s %-22s %-22s %s", "PAIR", "BEFORE", "AFTER", "COST")

	for _, pair := range pairs {
		wasPath, wasCost := describePath(g, before, pair, objective)
		nowPath, nowCost := describePath(g, after, pair, objective)

		mark := ""
		if wasPath != nowPath {
			mark = "  <- CHANGED"
			changed++
		}

		t.Logf("%-10s %-22s %-22s %g -> %g%s",
			string(pair[0])+"->"+string(pair[1]), wasPath, nowPath, wasCost, nowCost, mark)
	}

	t.Logf("%d of %d pairs moved", changed, len(pairs))
}

func describePath(g *Graph, costs map[EdgeID]*Cost, pair [2]NodeID, obj Objective) (string, float64) {
	path, err := computePath(g, costs, pair[0], pair[1], obj)
	if err != nil {
		return "unreachable", 0
	}

	hops := make([]string, len(path.Nodes))
	for i, n := range path.Nodes {
		hops[i] = string(n)
	}

	out := hops[0]
	for _, h := range hops[1:] {
		out += ">" + h
	}

	return out, path.Cost
}

func applyOverrides(g *Graph, base map[EdgeID]*Cost, overrides map[[2]NodeID]map[CostDimension]float64) map[EdgeID]*Cost {
	out := make(map[EdgeID]*Cost, len(base))
	for id, c := range base {
		out[id] = &Cost{FromEdge: c.FromEdge, ToEdge: c.ToEdge, Costs: maps.Clone(c.Costs)}
	}

	for pair, dims := range overrides {
		for id, edge := range g.edges {
			forward := edge.Local == pair[0] && edge.Remote == pair[1]
			reverse := edge.Local == pair[1] && edge.Remote == pair[0]
			if !forward && !reverse {
				continue
			}

			cost, exists := out[id]
			if !exists {
				continue
			}

			for dim, value := range dims {
				cost.Costs[dim] = value
			}
		}
	}

	return out
}

func allPairs(g *Graph) [][2]NodeID {
	nodes := slices.Sorted(maps.Keys(g.nodes))

	pairs := make([][2]NodeID, 0, len(nodes)*(len(nodes)-1))
	for _, src := range nodes {
		for _, dst := range nodes {
			if src != dst {
				pairs = append(pairs, [2]NodeID{src, dst})
			}
		}
	}

	return pairs
}
