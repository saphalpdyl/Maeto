package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"slices"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

type remoteSiteRoute struct {
	Remote *Site
	Path   *Path
}

func (c *Controller) startPCEUpdatesDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case paths := <-c.pceUpdates:
			// Convert PathKey(tenant, from, to, label) -> Path into, for each PE node and
			// its tenants, the routes [remote site prefix] -> segment list + color.
			// We need to get the egress PE's End.DT46 for the tenant as well
			//
			// Every remote site reachable over a path becomes one route on the
			// path's ingress PE, grouped here per PE and per tenant.
			remoteRoutesByNode := make(map[NodeID]map[TenantID][]remoteSiteRoute)
			for _, key := range slices.SortedFunc(maps.Keys(paths), PathKey.Compare) {
				path := paths[key]
				if path == nil || key.From == key.To {
					continue
				}

				if _, exists := c.inventory.Node(key.From); !exists {
					c.logger.ErrorContext(ctx, "found node without a inventory entry", log.NodeID(string(key.From)))
					continue
				}

				tenant, exists := c.tenants.Tenant(key.Tenant)
				if !exists {
					c.logger.ErrorContext(ctx, "path for unknown tenant", slog.Int("tenant", int(key.Tenant)))
					continue
				}

				byTenant, exists := remoteRoutesByNode[key.From]
				if !exists {
					byTenant = make(map[TenantID][]remoteSiteRoute)
					remoteRoutesByNode[key.From] = byTenant
				}

				// In the future, based on user input, a site prefix can be divided
				// into subnets with different policies
				for _, remoteSite := range tenant.Sites {
					if NodeID(remoteSite.Attach) != key.To {
						continue
					}

					byTenant[key.Tenant] = append(byTenant[key.Tenant], remoteSiteRoute{
						Remote: remoteSite,
						Path:   path,
					})
				}
			}

			tx := c.serviceRegistry.BeginTx()
			pceGraph := NewPCEGraph(c.topology.Graph().Clone())
			for nodeID, tenantsMap := range remoteRoutesByNode {
				localNode, exists := c.topology.GetNodeByID(nodeID)
				if !exists {
					c.logger.ErrorContext(ctx, "failed to get local node entry", log.NodeID(string(nodeID)))
					continue
				}

				for tenantID, routes := range tenantsMap {
					tenantSIDIntents := make([]nodesync.PESIDInstallIntent, 0)

					for _, route := range routes {

						remoteNode, exists := c.topology.GetNodeByID(NodeID(route.Remote.Attach))
						if !exists {
							c.logger.ErrorContext(ctx, "failed to get Node entry", log.NodeID(string(nodeID)))
							continue
						}

						dt46, err := c.serviceRegistry.GetOrGenerateSID(
							remoteNode.Locator,
							fmt.Sprintf("%d", tenantID),
							dataplane.EncapTypeDT46,
						)

						if err != nil {
							c.logger.ErrorContext(ctx, "failed to get/generated DT46 SID for tenant @ remote", slog.Int("tenant", int(tenantID)), log.NodeID(string(remoteNode.ID)))
							continue
						}

						sidAddrList, err := transitSegments(pceGraph, route.Path)
						if err != nil {
							c.logger.ErrorContext(ctx, "failed to build segment list; skipping site pair",
								slog.Int("tenant", int(tenantID)),
								log.NodeID(string(nodeID)),
								log.Err(err))
							continue
						}

						sidAddrList = append(sidAddrList, dt46)
						tenantSIDIntents = append(tenantSIDIntents, nodesync.PESIDInstallIntent{
							TenantID:     fmt.Sprint(tenantID),
							PrefixRoutes: []netip.Prefix{route.Remote.Prefix},
							Segments:     sidAddrList,
							Color:        route.Path.Label,
						})
					}

					c.serviceRegistry.StageSIDSegsForTenantOnNode(
						tx, fmt.Sprint(tenantID), nodeID, localNode.Locator, tenantSIDIntents,
					)
				}
			}

			err := tx.Commit(ctx)
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to commit transaction", log.Err(err))
			}
		}
	}
}
