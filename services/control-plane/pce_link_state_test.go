package controlplane

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func newLinkStateFixture() (*PCE, *Graph) {
	graph := &Graph{
		edges: map[EdgeID]*Edge{
			"fresh":    {ID: "fresh", Up: true},
			"stale":    {ID: "stale", Up: true},
			"unprobed": {ID: "unprobed", Up: true},
		},
	}

	costGraph := &CostGraph{
		graph:    map[CostGraphEdge]*Cost{},
		byID:     map[EdgeID]CostGraphEdge{},
		lastSeen: map[EdgeID]time.Time{},
	}

	pce := &PCE{
		costGraph: costGraph,
		logger:    slog.New(slog.DiscardHandler),
	}

	return pce, graph
}

func Test_UpdateLinkStatesMarksQuietLinksDown(t *testing.T) {
	pce, graph := newLinkStateFixture()

	pce.costGraph.MarkSeen("fresh", time.Now())
	pce.costGraph.MarkSeen("stale", time.Now().Add(-2*linkStaleAfter))

	pce.updateLinkStates(context.Background(), graph)

	if !graph.edges["fresh"].Up {
		t.Errorf("fresh link should stay up")
	}

	if graph.edges["stale"].Up {
		t.Errorf("stale link should be down")
	}

	if !graph.edges["unprobed"].Up {
		t.Errorf("a link that was never probed should keep its topology state")
	}
}

func Test_UpdateLinkStatesBringsLinkBackOnFreshSample(t *testing.T) {
	pce, graph := newLinkStateFixture()

	pce.costGraph.MarkSeen("stale", time.Now().Add(-2*linkStaleAfter))
	pce.updateLinkStates(context.Background(), graph)

	if graph.edges["stale"].Up {
		t.Fatalf("stale link should be down before the fresh sample")
	}

	pce.costGraph.MarkSeen("stale", time.Now())
	pce.updateLinkStates(context.Background(), graph)

	if !graph.edges["stale"].Up {
		t.Errorf("link should be back up after a fresh sample")
	}
}

func Test_DownLinkIsLeftOutOfThePCEGraph(t *testing.T) {
	pce, graph := newLinkStateFixture()
	graph.nodes = map[NodeID]*Node{"A": {ID: "A"}, "B": {ID: "B"}}
	graph.edges = map[EdgeID]*Edge{
		"A-B": {ID: "A-B", Local: "A", Remote: "B", Up: true},
	}

	pce.costGraph.MarkSeen("A-B", time.Now().Add(-2*linkStaleAfter))
	pce.updateLinkStates(context.Background(), graph)

	pceGraph := NewPCEGraph(graph)
	if len(pceGraph.edges) != 0 {
		t.Errorf("down link should not be in the PCE graph, got %d edges", len(pceGraph.edges))
	}
}
