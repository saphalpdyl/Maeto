package controlplane

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

type testLink struct {
	a, b   NodeID
	metric int
	delay  time.Duration
	up     bool
}

func buildTestGraph(links []testLink) *Graph {
	g := &Graph{
		nodes:    map[NodeID]*Node{},
		edges:    map[EdgeID]*Edge{},
		prefixes: map[string]*Prefix{},
		adj:      map[NodeID][]EdgeID{},
	}

	for _, l := range links {
		for _, n := range []NodeID{l.a, l.b} {
			if _, exists := g.nodes[n]; !exists {
				g.nodes[n] = &Node{ID: n, Name: string(n)}
			}
		}

		for _, dir := range [][2]NodeID{{l.a, l.b}, {l.b, l.a}} {
			id := EdgeID(string(dir[0]) + "-" + string(dir[1]))
			g.edges[id] = &Edge{
				ID:     id,
				Local:  dir[0],
				Remote: dir[1],
				Metric: l.metric,
				Delay:  l.delay,
				Up:     l.up,
			}
			g.adj[dir[0]] = append(g.adj[dir[0]], id)
		}
	}

	return g
}

func diamond() *Graph {
	return buildTestGraph([]testLink{
		{a: "A", b: "B", metric: 1, delay: 50 * time.Millisecond, up: true},
		{a: "B", b: "C", metric: 1, delay: 50 * time.Millisecond, up: true},
		{a: "A", b: "D", metric: 5, delay: 5 * time.Millisecond, up: true},
		{a: "D", b: "C", metric: 5, delay: 5 * time.Millisecond, up: true},
	})
}

func flatCosts(g *Graph, loss, latency, jitter float64) map[EdgeID]*Cost {
	costs := make(map[EdgeID]*Cost, len(g.edges))
	for id, edge := range g.edges {
		costs[id] = &Cost{
			FromEdge: edge,
			ToEdge:   edge,
			Costs: map[CostDimension]float64{
				COSTDIM_LOSS:    loss,
				COSTDIM_LATENCY: latency,
				COSTDIM_JITTER:  jitter,
			},
		}
	}

	return costs
}

func pathNodes(t *testing.T, g *Graph, costs map[EdgeID]*Cost, src, dst NodeID, obj Objective) []NodeID {
	t.Helper()

	path, err := computePath(g, costs, src, dst, obj)
	if err != nil {
		t.Fatalf("compute path %s -> %s: %v", src, dst, err)
	}

	return path.Nodes
}

func TestObjectiveSelectsPath(t *testing.T) {
	g := diamond()

	if got := pathNodes(t, g, nil, "A", "C", MinIGPMetric); !slices.Equal(got, []NodeID{"A", "B", "C"}) {
		t.Fatalf("MinIGPMetric: expected A B C, got %v", got)
	}

	if got := pathNodes(t, g, nil, "A", "C", MinDelay); !slices.Equal(got, []NodeID{"A", "D", "C"}) {
		t.Fatalf("MinDelay: expected A D C, got %v", got)
	}
}

func TestNilObjectiveDefaultsToIGPMetric(t *testing.T) {
	g := diamond()

	if got := pathNodes(t, g, nil, "A", "C", nil); !slices.Equal(got, []NodeID{"A", "B", "C"}) {
		t.Fatalf("expected A B C, got %v", got)
	}
}

func TestMinHopIgnoresMetrics(t *testing.T) {
	g := buildTestGraph([]testLink{
		{a: "A", b: "C", metric: 100, up: true},
		{a: "A", b: "B", metric: 1, up: true},
		{a: "B", b: "C", metric: 1, up: true},
	})

	if got := pathNodes(t, g, nil, "A", "C", MinHop); !slices.Equal(got, []NodeID{"A", "C"}) {
		t.Fatalf("expected direct A C, got %v", got)
	}

	if got := pathNodes(t, g, nil, "A", "C", MinIGPMetric); !slices.Equal(got, []NodeID{"A", "B", "C"}) {
		t.Fatalf("expected A B C, got %v", got)
	}
}

func TestMinDimensionFollowsArgusCost(t *testing.T) {
	g := diamond()
	costs := flatCosts(g, 1, 1, 1)

	costs["A-B"].Costs[COSTDIM_LATENCY] = 100
	costs["B-A"].Costs[COSTDIM_LATENCY] = 100

	if got := pathNodes(t, g, costs, "A", "C", MinDimension(COSTDIM_LATENCY)); !slices.Equal(got, []NodeID{"A", "D", "C"}) {
		t.Fatalf("expected A D C, got %v", got)
	}

	if got := pathNodes(t, g, costs, "A", "C", MinDimension(COSTDIM_LOSS)); !slices.Equal(got, []NodeID{"A", "B", "C"}) {
		t.Fatalf("expected A B C, got %v", got)
	}
}

func TestWeightedCostBlendsDimensions(t *testing.T) {
	g := diamond()
	costs := flatCosts(g, 0, 0, 0)

	costs["A-B"].Costs[COSTDIM_LATENCY] = 10
	costs["B-C"].Costs[COSTDIM_LATENCY] = 10
	costs["A-D"].Costs[COSTDIM_LOSS] = 3
	costs["D-C"].Costs[COSTDIM_LOSS] = 3

	latencyLed := WeightedCost(map[CostDimension]float64{COSTDIM_LATENCY: 1, COSTDIM_LOSS: 1})
	if got := pathNodes(t, g, costs, "A", "C", latencyLed); !slices.Equal(got, []NodeID{"A", "D", "C"}) {
		t.Fatalf("expected A D C, got %v", got)
	}

	lossLed := WeightedCost(map[CostDimension]float64{COSTDIM_LATENCY: 1, COSTDIM_LOSS: 10})
	if got := pathNodes(t, g, costs, "A", "C", lossLed); !slices.Equal(got, []NodeID{"A", "B", "C"}) {
		t.Fatalf("expected A B C, got %v", got)
	}
}

func TestUnmeasuredEdgeIsUnusable(t *testing.T) {
	g := diamond()
	costs := flatCosts(g, 1, 1, 1)

	delete(costs, "A-B")

	if got := pathNodes(t, g, costs, "A", "C", MinDimension(COSTDIM_LATENCY)); !slices.Equal(got, []NodeID{"A", "D", "C"}) {
		t.Fatalf("expected A D C, got %v", got)
	}

	if _, err := computePath(g, nil, "A", "C", MinDimension(COSTDIM_LATENCY)); !errors.Is(err, ErrNoPath) {
		t.Fatalf("expected ErrNoPath with no cost graph, got %v", err)
	}
}

func TestPathCost(t *testing.T) {
	path, err := computePath(diamond(), nil, "A", "C", MinDelay)
	if err != nil {
		t.Fatalf("compute path: %v", err)
	}

	if path.Cost != 10 {
		t.Fatalf("expected cost 10ms, got %v", path.Cost)
	}

	if len(path.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(path.Edges))
	}
}

func TestDownEdgeIsExcluded(t *testing.T) {
	g := diamond()
	g.edges["A-B"].Up = false

	if got := pathNodes(t, g, nil, "A", "C", MinIGPMetric); !slices.Equal(got, []NodeID{"A", "D", "C"}) {
		t.Fatalf("expected detour A D C, got %v", got)
	}
}

func TestInfiniteObjectivePrunesEdge(t *testing.T) {
	noSlowLinks := func(e *Edge, c *Cost) float64 {
		if e.Delay > 10*time.Millisecond {
			return math.Inf(1)
		}

		return MinIGPMetric(e, c)
	}

	if got := pathNodes(t, diamond(), nil, "A", "C", noSlowLinks); !slices.Equal(got, []NodeID{"A", "D", "C"}) {
		t.Fatalf("expected A D C, got %v", got)
	}
}

func TestUnreachableDestination(t *testing.T) {
	g := buildTestGraph([]testLink{
		{a: "A", b: "B", metric: 1, up: true},
		{a: "C", b: "D", metric: 1, up: true},
	})

	if _, err := computePath(g, nil, "A", "D", MinHop); !errors.Is(err, ErrNoPath) {
		t.Fatalf("expected ErrNoPath, got %v", err)
	}
}

func TestUnknownNode(t *testing.T) {
	g := diamond()

	if _, err := computePath(g, nil, "A", "Z", MinHop); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("expected ErrUnknownNode for dst, got %v", err)
	}

	if _, err := computePath(g, nil, "Z", "A", MinHop); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("expected ErrUnknownNode for src, got %v", err)
	}
}

func TestSameSourceAndDestination(t *testing.T) {
	path, err := computePath(diamond(), nil, "A", "A", MinHop)
	if err != nil {
		t.Fatalf("compute path: %v", err)
	}

	if !slices.Equal(path.Nodes, []NodeID{"A"}) || len(path.Edges) != 0 || path.Cost != 0 {
		t.Fatalf("expected a zero-length path at A, got %+v", path)
	}
}

func TestEqualCostTieBreakIsStable(t *testing.T) {
	g := loadTestGraph(t)

	costGraph, err := NewTestCostGraph(g)
	if err != nil {
		t.Fatalf("build cost graph: %v", err)
	}

	nodes := slices.Sorted(maps.Keys(g.nodes))
	src, dst := nodes[0], nodes[len(nodes)-1]

	costs := costGraph.Costs()
	want := pathNodes(t, g, costs, src, dst, MinDimension(COSTDIM_LATENCY))

	for i := range 100 {
		got := pathNodes(t, g, costs, src, dst, MinDimension(COSTDIM_LATENCY))
		if !slices.Equal(got, want) {
			t.Fatalf("run %d: %s -> %s returned %v, first run returned %v", i, src, dst, got, want)
		}
	}
}

func TestPathSetIsLoggable(t *testing.T) {
	g := diamond()

	path, err := computePath(g, nil, "A", "C", MinDelay)
	if err != nil {
		t.Fatalf("compute path: %v", err)
	}

	paths := make(PathSet)
	paths.Set("A", "C", COSTDIM_LATENCY, path)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("computed all paths", slog.Any("paths", paths))

	out := buf.String()
	if strings.Contains(out, "!ERROR") {
		t.Fatalf("handler failed to encode: %s", out)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid json: %v\n%s", err, out)
	}

	nested, ok := decoded["paths"].(map[string]any)["A"].(map[string]any)["C"].(map[string]any)["LATENCY"].(map[string]any)
	if !ok {
		t.Fatalf("expected paths.A.C.LATENCY group: %s", out)
	}

	if nested["path"] != "A>D>C" {
		t.Fatalf("expected hop list A>D>C, got %v", nested["path"])
	}
}
