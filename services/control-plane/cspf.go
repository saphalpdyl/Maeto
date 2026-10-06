package controlplane

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strings"
)

var (
	ErrNoPath      = errors.New("no path to destination")
	ErrUnknownNode = errors.New("node not in topology")
)

type Objective func(*PCEEdge, *Cost) float64

type Path struct {
	Nodes []NodeID    `json:"nodes"`
	Edges []PCEEdgeID `json:"pce_edges"`
	Links []EdgeID    `json:"edges"`
	Cost  float64     `json:"cost"`
}

func (p *Path) Equal(other *Path) bool {
	if (len(p.Edges) != len(other.Edges)) || (len(p.Nodes) != len(other.Nodes)) {
		return false
	}

	for i := range len(p.Nodes) {
		if p.Nodes[i] != other.Nodes[i] {
			return false
		}
	}

	for i := range len(p.Edges) {
		if p.Edges[i] != other.Edges[i] {
			return false
		}
	}

	return true
}

func (p *Path) LogValue() slog.Value {
	if p == nil {
		return slog.StringValue("<none>")
	}

	hops := make([]string, len(p.Nodes))
	for i, n := range p.Nodes {
		hops[i] = string(n)
	}

	return slog.GroupValue(
		slog.String("path", strings.Join(hops, ">")),
		slog.Float64("cost", p.Cost),
		slog.Int("hops", len(p.Edges)),
	)
}

type PathKey struct {
	Tenant TenantID
	From   NodeID
	To     NodeID
	Label  int64
	Dim    CostDimension
}

func (k PathKey) String() string {
	return fmt.Sprintf("%d/%s->%s/%d/%s", k.Tenant, k.From, k.To, k.Label, k.Dim)
}

func (k PathKey) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

func (k PathKey) Compare(other PathKey) int {
	return cmp.Or(
		cmp.Compare(k.Tenant, other.Tenant),
		cmp.Compare(k.From, other.From),
		cmp.Compare(k.To, other.To),
		cmp.Compare(k.Label, other.Label),
		cmp.Compare(k.Dim, other.Dim),
	)
}

type PathSet map[PathKey]*Path

func (ps PathSet) LogValue() slog.Value {
	attrs := make([]slog.Attr, 0, len(ps))
	for _, key := range slices.SortedFunc(maps.Keys(ps), PathKey.Compare) {
		attrs = append(attrs, slog.Any(key.String(), ps[key]))
	}

	return slog.GroupValue(attrs...)
}

func MinDimension(dim CostDimension) Objective {
	return WeightedCost(map[CostDimension]float64{dim: 1})
}

func WeightedCost(weights map[CostDimension]float64) Objective {
	return func(_ *PCEEdge, c *Cost) float64 {
		if c == nil {
			return math.Inf(1)
		}

		total := 0.0
		for _, dim := range slices.Sorted(maps.Keys(weights)) {
			total += weights[dim] * c.Costs[dim]
		}

		return total
	}
}

func (p *PCE) ComputePath(g *PCEGraph, src, dst NodeID, obj Objective) (*Path, error) {
	return computePath(g, p.costGraph.Costs(), src, dst, obj)
}

func computePath(g *PCEGraph, costs map[CostGraphEdge]*Cost, src, dst NodeID, obj Objective) (*Path, error) {
	if g == nil {
		return nil, errors.New("compute path: nil graph")
	}

	if obj == nil {
		return nil, errors.New("objective cannot be nil")
	}

	if _, exists := g.nodes[src]; !exists {
		return nil, fmt.Errorf("%w: %s", ErrUnknownNode, src)
	}

	if _, exists := g.nodes[dst]; !exists {
		return nil, fmt.Errorf("%w: %s", ErrUnknownNode, dst)
	}

	if src == dst {
		return &Path{Nodes: []NodeID{src}, Edges: []PCEEdgeID{}, Links: []EdgeID{}}, nil
	}

	nodes := slices.Sorted(maps.Keys(g.nodes))

	dist := make(map[NodeID]float64, len(nodes))
	prev := make(map[NodeID]PCEEdgeID, len(nodes))
	visited := make(map[NodeID]bool, len(nodes))

	for _, n := range nodes {
		dist[n] = math.Inf(1)
	}
	dist[src] = 0

	for {
		var current NodeID
		found := false
		best := math.Inf(1)

		for _, n := range nodes {
			if visited[n] || dist[n] >= best {
				continue
			}

			current, best, found = n, dist[n], true
		}

		if !found || current == dst {
			break
		}

		visited[current] = true

		for _, edgeID := range slices.SortedFunc(slices.Values(g.adj[current]), PCEEdgeID.Compare) {
			edge, exists := g.edges[edgeID]
			if !exists || visited[edge.NodeTo] {
				continue
			}

			if candidate := dist[current] + obj(edge, edge.Cost(costs)); candidate < dist[edge.NodeTo] {
				dist[edge.NodeTo] = candidate
				prev[edge.NodeTo] = edgeID
			}
		}
	}

	if math.IsInf(dist[dst], 1) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrNoPath, src, dst)
	}

	path, err := reconstruct(g, src, dst, prev)
	if err != nil {
		return nil, err
	}

	path.Cost = dist[dst]

	return path, nil
}

func reconstruct(g *PCEGraph, src, dst NodeID, prev map[NodeID]PCEEdgeID) (*Path, error) {
	path := &Path{
		Nodes: []NodeID{dst},
		Edges: []PCEEdgeID{},
		Links: []EdgeID{},
	}

	for current := dst; current != src; {
		edgeID, exists := prev[current]
		if !exists {
			return nil, fmt.Errorf("%w: broken predecessor chain at %s", ErrNoPath, current)
		}

		edge, exists := g.edges[edgeID]
		if !exists {
			return nil, fmt.Errorf("%w: predecessor edge %s missing", ErrNoPath, edgeID)
		}

		path.Edges = append(path.Edges, edgeID)
		path.Links = append(path.Links, edge.Links()...)

		current = edge.NodeFrom
		path.Nodes = append(path.Nodes, current)
	}

	slices.Reverse(path.Nodes)
	slices.Reverse(path.Edges)
	slices.Reverse(path.Links)

	return path, nil
}
