package controlplane

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

type PCEEdgeKind uint8

const (
	PCEEdgeBundle PCEEdgeKind = iota
	PCEEdgeLink
)

type PCEEdgeID struct {
	Kind  PCEEdgeKind
	From  NodeID
	To    NodeID
	Group string
	Link  EdgeID
}

func NewBundleID(from, to NodeID, group string) PCEEdgeID {
	return PCEEdgeID{Kind: PCEEdgeBundle, From: from, To: to, Group: group}
}

func NewLinkID(edge *Edge) PCEEdgeID {
	return PCEEdgeID{Kind: PCEEdgeLink, From: edge.Local, To: edge.Remote, Link: edge.ID}
}

func (id PCEEdgeID) String() string {
	if id.Kind == PCEEdgeLink {
		return "link:" + string(id.Link)
	}

	if id.Group == "" {
		return fmt.Sprintf("bundle:%s->%s", id.From, id.To)
	}

	return fmt.Sprintf("bundle:%s->%s#%s", id.From, id.To, id.Group)
}

func (id PCEEdgeID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id PCEEdgeID) Compare(other PCEEdgeID) int {
	return strings.Compare(id.String(), other.String())
}

type PCEEdge struct {
	ID        PCEEdgeID
	NodeFrom  NodeID
	NodeTo    NodeID
	RealEdges []*Edge
}

func (e *PCEEdge) IsBundle() bool {
	return e.ID.Kind == PCEEdgeBundle
}

func (e *PCEEdge) Capacity() float64 {
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

// PCEGraph is the abstracted representation of the actual topology graph that
// consolidates parallel links into bundles for easier ECMP-based path calaculation
// In the future, it will also support having explicitly marked non-ECMP node pairs
// that allow End.X SID assignments.
type PCEGraph struct {
	nodes map[NodeID]*Node
	edges map[PCEEdgeID]*PCEEdge
	adj   map[NodeID][]PCEEdgeID
}

func NewPCEGraph(g *Graph) *PCEGraph {
	pg := &PCEGraph{
		nodes: g.nodes,
		edges: make(map[PCEEdgeID]*PCEEdge),
		adj:   make(map[NodeID][]PCEEdgeID),
	}

	for _, id := range slices.Sorted(maps.Keys(g.edges)) {
		edge := g.edges[id]
		if !edge.Up {
			continue
		}

		//pceID := NewBundleID(edge.Local, edge.Remote, "")
		pceID := NewLinkID(edge)
		pceEdge, exists := pg.edges[pceID]
		if !exists {
			pceEdge = &PCEEdge{
				ID:       pceID,
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
