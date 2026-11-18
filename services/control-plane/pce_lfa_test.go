package controlplane

import (
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"testing"
)

func getTestGraph() *PCEGraph {
	g := &Graph{
		nodes:    map[NodeID]*Node{},
		edges:    map[EdgeID]*Edge{},
		prefixes: map[string]*Prefix{},
		adj:      map[NodeID][]EdgeID{},
	}

	for i, id := range []NodeID{"A", "B", "C", "D", "E"} {
		g.nodes[id] = &Node{
			ID:      id,
			Name:    string(id),
			Locator: netip.MustParsePrefix(fmt.Sprintf("fc00:0:%d::/48", i+1)),
		}
	}

	g.nodes["D"].ProtectorOf = []NodeID{"E"}
	g.nodes["E"].ProtectedBy = []NodeID{"D"}

	endX := 0
	half := func(id EdgeID, from, to NodeID) {
		endX++
		locator := g.nodes[from].Locator.Addr().As16()
		locator[15] = byte(endX)

		g.edges[id] = &Edge{
			ID:        id,
			Local:     from,
			Remote:    to,
			Bandwidth: 1000,
			Up:        true,
			EndX:      netip.AddrFrom16(locator),
		}
		g.adj[from] = append(g.adj[from], id)
	}

	link := func(a, b NodeID, member int) {
		half(EdgeID(fmt.Sprintf("%s-%s#%d", a, b, member)), a, b)
		half(EdgeID(fmt.Sprintf("%s-%s#%d", b, a, member)), b, a)
	}

	link("A", "E", 1)
	link("E", "D", 1)
	link("D", "C", 1)
	link("C", "B", 1)
	link("B", "A", 1)
	link("B", "A", 2)
	link("A", "D", 1)

	return NewPCEGraph(g)
}

func lid(from, to NodeID, member int) PCEEdgeID {
	return PCEEdgeID{
		Kind: PCEEdgeLink,
		From: from,
		To:   to,
		Link: EdgeID(fmt.Sprintf("%s-%s#%d", from, to, member)),
	}
}

func allTestEdges() []PCEEdgeID {
	return []PCEEdgeID{
		lid("A", "E", 1), lid("E", "A", 1),
		lid("E", "D", 1), lid("D", "E", 1),
		lid("D", "C", 1), lid("C", "D", 1),
		lid("C", "B", 1), lid("B", "C", 1),
		lid("B", "A", 1), lid("A", "B", 1),
		lid("B", "A", 2), lid("A", "B", 2),
		lid("A", "D", 1), lid("D", "A", 1),
	}
}

func without(all []PCEEdgeID, removed ...PCEEdgeID) []PCEEdgeID {
	return slices.DeleteFunc(slices.Clone(all), func(id PCEEdgeID) bool {
		return slices.Contains(removed, id)
	})
}

func checkPruned(t *testing.T, pruned *PCEGraph, wantNodes []NodeID, wantEdges []PCEEdgeID) {
	t.Helper()

	gotNodes := slices.Sorted(maps.Keys(pruned.nodes))
	slices.Sort(wantNodes)
	if !slices.Equal(gotNodes, wantNodes) {
		t.Errorf("nodes = %v, want %v", gotNodes, wantNodes)
	}

	gotEdges := slices.SortedFunc(maps.Keys(pruned.edges), PCEEdgeID.Compare)
	slices.SortFunc(wantEdges, PCEEdgeID.Compare)
	if !slices.Equal(gotEdges, wantEdges) {
		t.Errorf("edges = %v, want %v", gotEdges, wantEdges)
	}

	for id, edge := range pruned.edges {
		if _, ok := pruned.nodes[edge.NodeFrom]; !ok {
			t.Errorf("edge %s starts at removed node %s", id, edge.NodeFrom)
		}
		if _, ok := pruned.nodes[edge.NodeTo]; !ok {
			t.Errorf("edge %s ends at removed node %s", id, edge.NodeTo)
		}
		if !slices.Contains(pruned.adj[edge.NodeFrom], id) {
			t.Errorf("edge %s missing from adj[%s]", id, edge.NodeFrom)
		}
	}

	for node, ids := range pruned.adj {
		if _, ok := pruned.nodes[node]; !ok {
			t.Errorf("adj has removed node %s", node)
		}
		for _, id := range ids {
			if _, ok := pruned.edges[id]; !ok {
				t.Errorf("adj[%s] has removed edge %s", node, id)
			}
		}
	}
}

func checkUntouched(t *testing.T, g *PCEGraph) {
	t.Helper()

	if len(g.nodes) != 5 || len(g.edges) != len(allTestEdges()) {
		t.Errorf("input graph changed: %d nodes, %d edges", len(g.nodes), len(g.edges))
	}
}

func TestPruneGraphNodesOnly(t *testing.T) {
	g := getTestGraph()

	pruned := PruneGraph(g, []NodeID{"E"}, nil)

	checkPruned(t, pruned,
		[]NodeID{"A", "B", "C", "D"},
		without(allTestEdges(), lid("A", "E", 1), lid("E", "A", 1), lid("E", "D", 1), lid("D", "E", 1)),
	)
	checkUntouched(t, g)
}

func TestPruneGraphEdgesOnly(t *testing.T) {
	g := getTestGraph()

	pruned := PruneGraph(g, nil, []PCEEdgeID{lid("A", "B", 1), lid("B", "A", 1)})

	checkPruned(t, pruned,
		[]NodeID{"A", "B", "C", "D", "E"},
		without(allTestEdges(), lid("A", "B", 1), lid("B", "A", 1)),
	)
	checkUntouched(t, g)
}

func TestPruneGraphNodesAndEdgesDisjoint(t *testing.T) {
	g := getTestGraph()

	pruned := PruneGraph(g, []NodeID{"E"}, []PCEEdgeID{lid("C", "B", 1), lid("B", "C", 1)})

	checkPruned(t, pruned,
		[]NodeID{"A", "B", "C", "D"},
		without(allTestEdges(),
			lid("A", "E", 1), lid("E", "A", 1), lid("E", "D", 1), lid("D", "E", 1),
			lid("C", "B", 1), lid("B", "C", 1),
		),
	)
	checkUntouched(t, g)
}

func TestPruneGraphNodesAndEdgesOverlap(t *testing.T) {
	g := getTestGraph()

	pruned := PruneGraph(g, []NodeID{"E"}, []PCEEdgeID{lid("A", "E", 1), lid("D", "E", 1), lid("A", "D", 1)})

	checkPruned(t, pruned,
		[]NodeID{"A", "B", "C", "D"},
		without(allTestEdges(),
			lid("A", "E", 1), lid("E", "A", 1), lid("E", "D", 1), lid("D", "E", 1),
			lid("A", "D", 1),
		),
	)
	checkUntouched(t, g)
}
