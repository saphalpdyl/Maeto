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

// Convert PathKey(tenant, from, to, label) -> Path into, for each PE node and
// its tenants, the routes [remote site prefix] -> segment list + color.
// We need to get the egress PE's End.DT46 for the tenant as well
//
// Every remote site reachable over a path becomes one route on the
// path's ingress PE, grouped here per PE and per tenant.
func (c *Controller) buildRemoteRoutesByNode(ctx context.Context, paths PathSet) map[NodeID]map[TenantID][]remoteSiteRoute {
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
			if NodeID(remoteSite.AttachNodeID) != key.To {
				continue
			}

			byTenant[key.Tenant] = append(byTenant[key.Tenant], remoteSiteRoute{
				Remote: remoteSite,
				Path:   path,
			})
		}
	}
	return remoteRoutesByNode
}

func (c *Controller) buildAndStageSegments(ctx context.Context, remoteRoutesByNode map[NodeID]map[TenantID][]remoteSiteRoute, tx *RegistryTx) {
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

				remoteNode, exists := c.topology.GetNodeByID(NodeID(route.Remote.AttachNodeID))
				if !exists {
					c.logger.ErrorContext(ctx, "failed to get Node entry", log.NodeID(string(nodeID)))
					continue
				}

				dt46, err := c.serviceRegistry.GetOrGenerateSID(
					ctx,
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

			c.serviceRegistry.StageSIDSegsForTenantOnNode(ctx, tx, fmt.Sprint(tenantID), nodeID, localNode.Locator, tenantSIDIntents)
		}
	}
}

// transitSegments gives one SID per transit node of path: End.X out of the
// path's link when the node has reported one, End otherwise. The headend gets
// no SID because Linux cannot execute its own SIDs, so the first hop is plain
// routing towards the second node. The egress is reached by the DT46 the
// caller appends.
func transitSegments(graph *PCEGraph, path *Path) ([]netip.Addr, error) {
	if len(path.Edges) != len(path.Nodes)-1 {
		return nil, fmt.Errorf("path has %d nodes but %d edges", len(path.Nodes), len(path.Edges))
	}

	segments := make([]netip.Addr, 0, len(path.Nodes))

	for i := 1; i < len(path.Nodes)-1; i++ {
		node, exists := graph.nodes[path.Nodes[i]]
		if !exists {
			return nil, fmt.Errorf("transit node %s is not in the topology", path.Nodes[i])
		}

		endX, ok := endXOf(graph, path.Edges[i])
		if ok {
			segments = append(segments, endX)
			continue
		}

		segments = append(segments, node.Locator.Addr())
	}

	return segments, nil
}

// endXOf only answers for a single link that is still in the graph. A bundle,
// a link that went down, or a node that has not reported yet falls back to End,
// which lets the IGP pick the way out.
func endXOf(graph *PCEGraph, id PCEEdgeID) (netip.Addr, bool) {
	edge, exists := graph.edges[id]
	if !exists || edge.IsBundle() || len(edge.RealEdges) != 1 {
		return netip.Addr{}, false
	}

	endX := edge.RealEdges[0].EndX

	return endX, endX.IsValid()
}
