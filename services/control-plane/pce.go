package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

type PCE struct {
	costGraph  *CostGraph
	reportChan chan<- PathSet
	logger     *slog.Logger
}

func NewPCE(costGraph *CostGraph, reportChan chan<- PathSet, logger *slog.Logger) *PCE {
	return &PCE{
		costGraph:  costGraph,
		logger:     logger,
		reportChan: reportChan,
	}
}

func (p *PCE) Run(ctx context.Context, graph *Graph) {
	ticker := time.NewTicker(3 * time.Second)

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Stopping PCE ticker")
			ticker.Stop()
			return
		case <-ticker.C:
			paths := make(PathSet)

			graph.mu.RLock()
			for _, srcNode := range graph.nodes {
				for _, destNode := range graph.nodes {
					if srcNode.ID == destNode.ID {
						continue
					}

					path, err := p.ComputePath(graph, srcNode.ID, destNode.ID, func(e *Edge, c *Cost) float64 {
						return c.Costs[COSTDIM_LATENCY]
					})

					if err != nil {
						p.logger.ErrorContext(
							ctx,
							"failed to compute paths",
							log.Err(err),
							slog.String("fromNode", string(srcNode.ID)),
							slog.String("toNode", string(destNode.ID)),
						)
						continue
					}

					paths.Set(srcNode.ID, destNode.ID, COSTDIM_LATENCY, path)
				}
			}

			p.logger.InfoContext(ctx, "computed all paths", slog.Any("paths", paths))
			graph.mu.RUnlock()

			p.reportChan <- paths

		}
	}
}
