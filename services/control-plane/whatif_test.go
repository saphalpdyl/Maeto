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
	pg := NewPCEGraph(g)

	costGraph, err := NewTestCostGraph(g)
	if err != nil {
		t.Fatalf("build cost graph: %v", err)
	}

	before := costGraph.Costs()
	after := applyOverrides(before, whatIf.Overrides)

	objective := whatIf.Objective

	pairs := whatIf.Pairs
	if len(pairs) == 0 {
		pairs = allPairs(pg)
	}

	changed := 0
	t.Logf("%-10s %-22s %-22s %s", "PAIR", "BEFORE", "AFTER", "COST")

	for _, pair := range pairs {
		wasPath, wasCost := describePath(pg, before, pair, objective)
		nowPath, nowCost := describePath(pg, after, pair, objective)

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

func describePath(g *PCEGraph, costs map[CostGraphEdge]*Cost, pair [2]NodeID, obj Objective) (string, float64) {
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

func applyOverrides(base map[CostGraphEdge]*Cost, overrides map[[2]NodeID]map[CostDimension]float64) map[CostGraphEdge]*Cost {
	out := make(map[CostGraphEdge]*Cost, len(base))
	for key, c := range base {
		out[key] = &Cost{Edge: c.Edge, Costs: maps.Clone(c.Costs)}
	}

	for pair, dims := range overrides {
		for key, cost := range out {
			forward := key.From == pair[0] && key.To == pair[1]
			reverse := key.From == pair[1] && key.To == pair[0]
			if !forward && !reverse {
				continue
			}

			for dim, value := range dims {
				cost.Costs[dim] = value
			}
		}
	}

	return out
}

func allPairs(g *PCEGraph) [][2]NodeID {
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
