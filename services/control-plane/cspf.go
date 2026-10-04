package controlplane

import (
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
	Nodes []NodeID `json:"nodes"`
	Edges []string `json:"pce_edges"`
	Links []EdgeID `json:"edges"`
	Cost  float64  `json:"cost"`
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

type PathSet map[NodeID]map[NodeID]map[CostDimension]*Path

func (ps PathSet) Set(src, dst NodeID, dim CostDimension, path *Path) {
	byDst, exists := ps[src]
	if !exists {
		byDst = make(map[NodeID]map[CostDimension]*Path)
		ps[src] = byDst
	}

	byDim, exists := byDst[dst]
	if !exists {
		byDim = make(map[CostDimension]*Path)
		byDst[dst] = byDim
	}

	byDim[dim] = path
}

func (ps PathSet) LogValue() slog.Value {
	sources := make([]slog.Attr, 0, len(ps))

	for _, src := range slices.Sorted(maps.Keys(ps)) {
		dests := make([]slog.Attr, 0, len(ps[src]))

		for _, dst := range slices.Sorted(maps.Keys(ps[src])) {
			dims := make([]slog.Attr, 0, len(ps[src][dst]))

			for _, dim := range slices.Sorted(maps.Keys(ps[src][dst])) {
				dims = append(dims, slog.Any(string(dim), ps[src][dst][dim]))
			}

			dests = append(dests, slog.Attr{Key: string(dst), Value: slog.GroupValue(dims...)})
		}

		sources = append(sources, slog.Attr{Key: string(src), Value: slog.GroupValue(dests...)})
	}

	return slog.GroupValue(sources...)
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
		return &Path{Nodes: []NodeID{src}, Edges: []string{}, Links: []EdgeID{}}, nil
	}

	nodes := slices.Sorted(maps.Keys(g.nodes))

	dist := make(map[NodeID]float64, len(nodes))
	prev := make(map[NodeID]string, len(nodes))
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

		for _, edgeID := range slices.Sorted(slices.Values(g.adj[current])) {
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

func reconstruct(g *PCEGraph, src, dst NodeID, prev map[NodeID]string) (*Path, error) {
	path := &Path{
		Nodes: []NodeID{dst},
		Edges: []string{},
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
