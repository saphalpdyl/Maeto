// Holds information on cost mapping derived from Argus' findings
package controlplane

import (
	"fmt"
	"maps"
	"sync"
	"time"
)

const MaxCostGraphWaitTillReady = 5 * time.Minute

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

	lastSeen map[EdgeID]time.Time

	convergedEdges map[EdgeID]struct{}
	startedAt      time.Time
}

type CostDimension string

const (
	COSTDIM_LOSS    CostDimension = "LOSS"
	COSTDIM_LATENCY CostDimension = "LATENCY"
	COSTDIM_JITTER  CostDimension = "JITTER"
)

type Cost struct {
	Costs map[CostDimension]float64
}

func NewTestCostGraph(
	g *Graph,
) (*CostGraph, error) {
	cg := &CostGraph{
		graph:          make(map[CostGraphEdge]*Cost),
		byID:           make(map[EdgeID]CostGraphEdge),
		lastSeen:       make(map[EdgeID]time.Time),
		convergedEdges: make(map[EdgeID]struct{}),
		startedAt:      time.Now(),
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

func (c *CostGraph) MarkSeen(id EdgeID, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastSeen[id] = at
}

func (c *CostGraph) LastSeen() map[EdgeID]time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return maps.Clone(c.lastSeen)
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
			Costs: maps.Clone(cost.Costs),
		}
	}

	return costs
}

func (c *CostGraph) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if time.Since(c.startedAt) > MaxCostGraphWaitTillReady {
		return true
	}

	for edgeID := range c.byID {
		if _, exists := c.convergedEdges[edgeID]; !exists {
			return false
		}
	}

	return true
}

// MarkEdgeConverged marks an edge whose EWMAs have seen enough samples to be trusted
func (c *CostGraph) MarkEdgeConverged(id EdgeID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.convergedEdges[id] = struct{}{}
}
