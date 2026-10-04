// Holds information on cost mapping derived from Argus' findings
package controlplane

import (
	"fmt"
	"maps"
	"sync"
)

type CostGraphEdge struct {
	ID   EdgeID
	From NodeID
	To   NodeID
}

func CostKey(e *Edge) CostGraphEdge {
	return CostGraphEdge{ID: e.ID, From: e.Local, To: e.Remote}
}

type CostGraph struct {
	mu    sync.RWMutex
	graph map[CostGraphEdge]*Cost // interface->interface pair
	byID  map[EdgeID]CostGraphEdge
}

type CostDimension string

const (
	COSTDIM_LOSS    CostDimension = "LOSS"
	COSTDIM_LATENCY CostDimension = "LATENCY"
	COSTDIM_JITTER  CostDimension = "JITTER"
)

type Cost struct {
	Edge  *Edge
	Costs map[CostDimension]float64
}

func NewTestCostGraph(
	g *Graph,
) (*CostGraph, error) {
	cg := &CostGraph{
		graph: make(map[CostGraphEdge]*Cost),
		byID:  make(map[EdgeID]CostGraphEdge),
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

			key := CostKey(edge)
			if _, exists = cg.graph[key]; exists {
				continue
			}

			cg.byID[e] = key
			cg.graph[key] = &Cost{
				Edge: edge,
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

func (c *CostGraph) UpdateCost(id EdgeID, dim CostDimension, cost float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key, exists := c.byID[id]
	if !exists {
		return
	}

	c.graph[key].Costs[dim] = cost
}

func CostsBetween(costs map[CostGraphEdge]*Cost, from, to NodeID) map[CostGraphEdge]*Cost {
	out := make(map[CostGraphEdge]*Cost)
	for key, cost := range costs {
		if key.From == from && key.To == to {
			out[key] = cost
		}
	}

	return out
}

func (c *CostGraph) Costs() map[CostGraphEdge]*Cost {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	costs := make(map[CostGraphEdge]*Cost, len(c.graph))
	for id, cost := range c.graph {
		costs[id] = &Cost{
			Edge:  cost.Edge,
			Costs: maps.Clone(cost.Costs),
		}
	}

	return costs
}
