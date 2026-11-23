package controlplane

import (
	"context"

	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

type PCEUpdate struct {
	RepairList LFARepairList
	Paths      PathSet

	// Need a snapshot of the graph to be used throughout the tick. Without this, we get nasty race conditions
	Topology *Graph
}

type remoteSiteRoute struct {
	Remote *Site
	Path   *Path
}

func (c *Controller) startPCEUpdatesDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-c.pceUpdates:
			paths := update.Paths

			remoteRoutesByNode := c.buildRemoteRoutesByNode(ctx, paths)
			tx := c.serviceRegistry.BeginTx()

			// intents staged inside the method
			c.buildAndStageSegments(ctx, remoteRoutesByNode, tx)
			c.buildAndStageLFARepairSegments(ctx, update.RepairList, tx)
			c.buildAndStageStandbyActivation(ctx, c.topology.graph, tx)
			c.serviceRegistry.StageDownNodes(tx, c.topology.Graph().DownNodes())

			err := tx.Commit(ctx)
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to commit transaction", log.Err(err))
			}
		}
	}
}
