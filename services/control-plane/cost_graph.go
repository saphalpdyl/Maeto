// Holds information on cost mapping derived from Argus' findings
package controlplane

import "sync"

type CostGraph struct {
	mu    sync.RWMutex        //nolint:unused // guards graph once concurrent access lands
	graph map[[2]EdgeID]*Cost // interface->interface pair
}

func NewCostGraph() *CostGraph {
	return &CostGraph{
		graph: map[[2]EdgeID]*Cost{},
	}
}

type CostDimension string

const (
	COSTDIM_LOSS    CostDimension = "LOSS"
	COSTDIM_LATENCY CostDimension = "LATENCY"
	COSTDIM_JITTER  CostDimension = "JITTER"
)

type Cost struct {
	FromEdge *Edge
	ToEdge   *Edge

	Dim   CostDimension
	Value float64
}
