package controlplane

import (
	"context"
)

type PCE struct {
	costGraph *CostGraph
}

func NewPCE() *PCE {
	return &PCE{
		costGraph: NewCostGraph(),
	}
}

func (p *PCE) CostGraph() *CostGraph {
	return p.costGraph
}

func (p *PCE) Run(ctx context.Context, graph *Graph) {
	<-ctx.Done()
}
