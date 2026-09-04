// Holds information on cost mapping derived from Argus' findings
package controlplane

import (
	"fmt"
	"maps"
	"sync"
)

type CostGraph struct {
	mu    sync.RWMutex
	graph map[EdgeID]*Cost // interface->interface pair
}

// func NewCostGraph() *CostGraph {
// 	return &CostGraph{
// 		graph: make(map[[2]NodeID]map[CostDimension]*Cost),
// 	}
// }

type CostDimension string

const (
	COSTDIM_LOSS    CostDimension = "LOSS"
	COSTDIM_LATENCY CostDimension = "LATENCY"
	COSTDIM_JITTER  CostDimension = "JITTER"
)

type Cost struct {
	FromEdge *Edge
	ToEdge   *Edge

	Costs map[CostDimension]float64
}

func NewTestCostGraph(
	g *Graph,
) (*CostGraph, error) {
	cg := &CostGraph{
		graph: make(map[EdgeID]*Cost),
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	for id := range g.nodes {
		adjEdges := g.adj[id]
		for _, e := range adjEdges {
			edge, exists := g.edges[e]
			if !exists {
				return nil, fmt.Errorf("Edge %s does not exists", string(e))
			}

			_, exists = cg.graph[e]
			if exists {
				continue
			}

			cg.graph[e] = &Cost{
				FromEdge: edge,
				ToEdge:   edge,
				Costs: map[CostDimension]float64{
					COSTDIM_LOSS:    5,
					COSTDIM_LATENCY: 5,
					COSTDIM_JITTER:  5,
				},
			}
		}
	}

	return cg, nil
}

func (c *CostGraph) Costs() map[EdgeID]*Cost {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return maps.Clone(c.graph)
}
