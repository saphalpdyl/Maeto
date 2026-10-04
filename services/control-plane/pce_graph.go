package controlplane

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

type CanonicalEdgeID string

func NewCanonicalEdgeID(a, b string) CanonicalEdgeID {
	if b < a {
		a, b = b, a
	}

	return CanonicalEdgeID(fmt.Sprintf("%s-%s", a, b))
}

type PCEEdge struct {
	ID        string // Can be bundled edge C-D or single edge with interface names C:eth4-D:eth1
	IsBundle  bool
	NodeFrom  NodeID
	NodeTo    NodeID
	RealEdges []*Edge
}

func (e *PCEEdge) CanonicalID() CanonicalEdgeID {
	if e.IsBundle {
		return NewCanonicalEdgeID(string(e.NodeFrom), string(e.NodeTo))
	}

	ids := strings.Split(e.ID, "-") // gives: ["C:eth1", "D:eth2"]
	if len(ids) != 2 {
		return "incorrect-id"
	}

	return NewCanonicalEdgeID(ids[0], ids[1])
}

func (e *PCEEdge) Bandwidth() float64 {
	if len(e.RealEdges) == 0 {
		return 0
	}

	smallest := math.Inf(1)
	for _, edge := range e.RealEdges {
		smallest = math.Min(smallest, edge.Bandwidth)
	}

	return float64(len(e.RealEdges)) * smallest
}

func (e *PCEEdge) Links() []EdgeID {
	ids := make([]EdgeID, len(e.RealEdges))
	for i, edge := range e.RealEdges {
		ids[i] = edge.ID
	}

	return ids
}

func (e *PCEEdge) Cost(costs map[CostGraphEdge]*Cost) *Cost {
	var worst *Cost
	for _, edge := range e.RealEdges {
		cost, exists := costs[CostKey(edge)]
		if !exists || cost == nil {
			continue
		}

		if worst == nil {
			worst = &Cost{Costs: maps.Clone(cost.Costs)}
			continue
		}

		for dim, value := range cost.Costs {
			if current, set := worst.Costs[dim]; !set || value > current {
				worst.Costs[dim] = value
			}
		}
	}

	return worst
}

func NewPCEEdgeID(from, to NodeID) string {
	return fmt.Sprintf("%s->%s", from, to)
}

// PCEGraph is the abstracted representation of the actual topology graph that
// consolidates parallel links into bundles for easier ECMP-based path calaculation
// In the future, it will also support having explicitly marked non-ECMP node pairs
// that allow End.X SID assignments.
type PCEGraph struct {
	nodes map[NodeID]*Node
	edges map[string]*PCEEdge
	adj   map[NodeID][]string
}

func NewPCEGraph(g *Graph) *PCEGraph {
	pg := &PCEGraph{
		nodes: g.nodes,
		edges: make(map[string]*PCEEdge),
		adj:   make(map[NodeID][]string),
	}

	for _, id := range slices.Sorted(maps.Keys(g.edges)) {
		edge := g.edges[id]
		if !edge.Up {
			continue
		}

		pceID := NewPCEEdgeID(edge.Local, edge.Remote)
		pceEdge, exists := pg.edges[pceID]
		if !exists {
			pceEdge = &PCEEdge{
				ID:       pceID,
				IsBundle: true,
				NodeFrom: edge.Local,
				NodeTo:   edge.Remote,
			}
			pg.edges[pceID] = pceEdge
			pg.adj[edge.Local] = append(pg.adj[edge.Local], pceID)
		}

		pceEdge.RealEdges = append(pceEdge.RealEdges, edge)
	}

	return pg
}
