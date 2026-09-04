package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/intentkv"
	"github.com/saphalpdyl/maeto/libs/statekv"
	log "github.com/saphalpdyl/maeto/services/control-plane/log"
)

type Controller struct {
	config Config
	logger *slog.Logger
	js     jetstream.JetStream

	topology        *ClabTopologyManager
	inventory       NodeInventory
	tenants         TenantRepository
	serviceRegistry *ServiceRegistry
	pce             *PCE

	ready bool

	pceUpdates chan PathSet
}

// Data reported by the node
type NodeReport struct {
	Node NodeID
	Seq  uint64

	Locator netip.Prefix

	SystemID string
	AdjSIDs  []AdjacencySID
}

type AdjacencySID struct {
	SID          netip.Addr
	PeerSystemID string
}

func NewController(
	ctx context.Context,
	js jetstream.JetStream,
	config Config,
	logger *slog.Logger,
) (*Controller, error) {
	topology := NewClabTopologyManager(ClabTopologyConfig{
		StatePath:       config.StatePath,
		TopologyDirPath: config.DataDir,
	})
	if err := topology.LoadTopology(); err != nil {
		return nil, fmt.Errorf("failed to load topology: %w", err)
	}

	inventory := NewJSONNodeInventory(NodeInventoryConfig{
		StatePath:       config.StatePath,
		TopologyDirPath: config.DataDir,
	})
	if err := inventory.Load(ctx); err != nil {
		return nil, fmt.Errorf("failed to load node inventory: %w", err)
	}

	tenants := NewJSONTenantRepository(TenantRepositoryConfig{
		StatePath:       config.StatePath,
		TopologyDirPath: config.DataDir,
	})
	if err := tenants.Load(ctx); err != nil {
		return nil, fmt.Errorf("failed to load tenants: %w", err)
	}

	logger.InfoContext(ctx, "controller initialized",
		slog.Int("tenants", len(tenants.Tenants())),
	)

	// Intent KV
	intentPublisher, err := intentkv.NewPublisher(ctx, js)
	if err != nil {
		return nil, fmt.Errorf("failed to create intent publisher: %w", err)
	}

	costGraph, err := NewTestCostGraph(topology.Graph())
	if err != nil {
		return nil, fmt.Errorf("failed to create cost graph: %w", err)
	}

	sr := NewServiceRegistry(
		&ServiceRegistryConfig{},
		intentPublisher,
		logger.With(log.Domain(log.DomainServiceRegistry)),
	)

	pceUpdatesChan := make(chan PathSet, 32)

	return &Controller{
		config: config,
		logger: logger,
		js:     js,

		topology:        topology,
		inventory:       inventory,
		tenants:         tenants,
		serviceRegistry: sr,
		pce: NewPCE(
			costGraph,
			pceUpdatesChan,
			logger.With(log.Domain(log.DomainPCE)),
		),
		pceUpdates: pceUpdatesChan,

		ready: false,
	}, nil
}

func (c *Controller) Start(ctx context.Context) {
	c.logger.InfoContext(ctx, "controller starting", log.ListenAddress(c.config.ListenAddress))

	c.js.Conn().SetReconnectHandler(func(_ *nats.Conn) {
		c.logger.WarnContext(ctx, "nats reconnected, restoring jetstream state",
			log.Domain(log.DomainControlPlaneLifecycle),
		)

		if err := c.serviceRegistry.Restore(ctx); err != nil {
			c.logger.ErrorContext(ctx, "failed to restore jetstream state",
				log.Domain(log.DomainControlPlaneLifecycle),
				log.Err(err),
			)
		}
	})

	go c.pce.Run(ctx, c.topology.Graph())

	if err := c.startSnapshotPublisher(ctx); err != nil {
		c.logger.ErrorContext(ctx, "failed to start snapshot publisher",
			log.Domain(log.DomainControlPlaneLifecycle),
			log.Err(err),
		)
	}

	if err := c.setupHealthEndpoint(ctx); err != nil {
		return
	}

	if err := c.setupPortalAuthEndpoint(ctx); err != nil {
		return
	}

	if err := c.setupPETunnelUpdate(ctx); err != nil {
		return
	}

	if err := c.setupCPETunnelUpdate(ctx); err != nil {
		return
	}

	go c.startPCEUpdatesDispatcher(ctx)
	c.ready = true

}

func (c *Controller) setupHealthEndpoint(ctx context.Context) error {
	_, err := c.js.Conn().Subscribe("maeto.control.health.ready", func(msg *nats.Msg) {
		isReady := "false"
		if c.ready {
			isReady = "true"
		}

		data, _ := json.Marshal(map[string]string{
			"sub":   "maeto.control.health.ready",
			"ready": isReady,
		})

		if err := msg.Respond(data); err != nil {
			c.logger.ErrorContext(ctx, "failed to send reply", log.Err(err))
		}
	})

	if err != nil {
		c.logger.ErrorContext(ctx, "failed to subscribe to maeto.control.health.ready", log.Err(err))
		return err
	}

	return nil
}

func (c *Controller) setupPortalAuthEndpoint(ctx context.Context) error {
	_, err := c.js.Conn().Subscribe(controlapi.SubjectPortalAuthIdentity, func(msg *nats.Msg) {
		var req controlapi.PortalAuthEndpointRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			c.logger.ErrorContext(ctx, "failed to unmarshal request", log.Err(err))
			return
		}

		site, exists := c.tenants.SiteByPortalID(req.PortalID)
		if !exists {
			c.logger.ErrorContext(ctx, "portal identity not found")
			return
		}

		attachNode, exists := c.inventory.Node(NodeID(site.Attach))
		if !exists {
			c.logger.ErrorContext(ctx, "attach node not found in inventory", log.NodeID(site.Attach))
			return
		}

		if !attachNode.HasAccessSide() {
			c.logger.ErrorContext(ctx, "attach node has no access side", log.NodeID(site.Attach))
			return
		}

		data, err := json.Marshal(controlapi.PortalAuthEndpointResponse{
			LocalSwanIdentity:  site.Identity,
			RemoteSwanIdentity: fmt.Sprintf("%s.maeto.net", attachNode.Name),
			AttachNode:         site.AttachNode,
			Prefix:             site.Prefix.String(),
			AttachNodeAddr:     attachNode.Access.Address.Addr().String(),
		})
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to marshal response", log.Err(err))
			return
		}

		if err := msg.Respond(data); err != nil {
			c.logger.ErrorContext(ctx, "failed to send reply", log.Err(err))
			return
		}

		tenant, exists := c.tenants.Tenant(site.TenantID)
		if !exists {
			c.logger.ErrorContext(ctx, "couldn't find tenant before assinging CPEIntent", slog.Int("tenant_id", site.TenantID))
			return
		}

		// Create the intent if not exist
		c.serviceRegistry.mu.Lock()
		_, exists = c.serviceRegistry.registry[req.PortalID]
		if !exists {
			cpeIntent := dataplane.CPEIntent{
				TunnelInterfaceID:    0, // empty intent, 0 means no tunnel yet
				TunnelPE:             site.AttachNode,
				TunnelPEEndpointAddr: attachNode.Access.Address.Addr(),
				TenantID:             fmt.Sprintf("%d", site.TenantID),
				TenantPrefix:         tenant.Allocation,
				SitePrefix:           site.Prefix,
			}

			c.serviceRegistry.registry[req.PortalID] = &dataplane.NodeIntent{
				NodeType:   dataplane.NodeTypeCPE,
				Intent:     &cpeIntent,
				Timestamp:  time.Now(),
				Generation: 1,
				Version:    1,
			}
		}

		c.serviceRegistry.mu.Unlock()
	})

	if err != nil {
		c.logger.ErrorContext(ctx, "failed to subscribe to maeto.control.portal.auth.identity", log.Err(err))
		return err
	}

	return nil
}

// This endpoint listens to updates from CPE on child-updown events from vici
// Each events carries the new if_id and the portal_id for identification.
// It updates the service_registry[portal_id] with the new if_id which pushes
// a new intent to the CPE
func (c *Controller) setupCPETunnelUpdate(ctx context.Context) error {
	_, err := c.js.Conn().Subscribe(controlapi.SubjectCPETunnelUpdate, func(msg *nats.Msg) {

		err := c.handleCPETunnelUpdate(ctx, msg.Data)
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to generate new intent")
		}

		successResponse, err := json.Marshal(controlapi.TunnelUpdateResponse{Ok: err == nil})
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to marshal response", log.Err(err))
			return
		}
		err = msg.Respond(successResponse)
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to send response", log.Err(err))
		}
	})

	if err != nil {
		c.logger.ErrorContext(ctx, "failed to subscribe to cpe.tunnel_update subject", log.Err(err))
		return err
	}

	return nil
}

func (c *Controller) handleCPETunnelUpdate(ctx context.Context, data []byte) error {
	var req controlapi.CPETunnelUpdateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		c.logger.ErrorContext(ctx, "failed to unmarshal request", log.Err(err))
		return fmt.Errorf("failed to unmarshal request")
	}

	// Get tenant
	site, exists := c.tenants.SiteByPortalID(req.PortalID)
	if !exists {
		return fmt.Errorf("portal identity not found")
	}

	tenant, exists := c.tenants.Tenant(site.TenantID)
	if !exists {
		return fmt.Errorf("tenant identity not found")
	}

	attachNode, exists := c.inventory.Node(NodeID(site.Attach))
	if !exists {
		return fmt.Errorf("attach node not found in inventory")
	}

	cpeIntent := dataplane.CPEIntent{
		TunnelInterfaceID:    req.IfID,
		TunnelPE:             site.AttachNode,
		TunnelPEEndpointAddr: attachNode.Access.Address.Addr(),
		TenantID:             fmt.Sprintf("%d", site.TenantID),
		TenantPrefix:         tenant.Allocation,
		SitePrefix:           site.Prefix,
		PortalID:             req.PortalID,
	}

	err := c.serviceRegistry.UpsertCPEIntentForSite(
		ctx, fmt.Sprintf("%d", tenant.ID), req.PortalID, &cpeIntent,
	)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to upsert CPE intent for site", log.Err(err))
		return err
	}

	return nil
}

// This endpoint is basically the same as CPETunnelUpdate but for PEs
// The endpoints are separated for the future where we have different NATS instances
// for owned and unowned boundaries.
func (c *Controller) setupPETunnelUpdate(ctx context.Context) error {
	_, err := c.js.Conn().Subscribe(controlapi.SubjectPETunnelUpdate, func(msg *nats.Msg) {
		err := c.handlePETunnelUpdate(ctx, msg.Data)
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to generate new intent")
		}

		successResponse, err := json.Marshal(controlapi.TunnelUpdateResponse{Ok: err == nil})
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to marshal response", log.Err(err))
			return
		}
		err = msg.Respond(successResponse)
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to send response", log.Err(err))
		}
	})
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to subscribe to maeto.control.push.tunnel.initiate", log.Err(err))
		return err
	}

	return nil
}

func (c *Controller) handlePETunnelUpdate(ctx context.Context, data []byte) error {
	var req controlapi.PETunnelUpdateRequest
	if err := json.Unmarshal(data, &req); err != nil {
		c.logger.ErrorContext(ctx, "failed to unmarshal request", log.Err(err))
		return fmt.Errorf("failed to unmarshal request: %w", err)
	}

	// Get tenant
	site, exists := c.tenants.SiteByPortalID(req.PortalID)
	if !exists {
		return fmt.Errorf("portal identity not found")

	}

	tenant, exists := c.tenants.Tenant(site.TenantID)
	if !exists {
		return fmt.Errorf("tenant not found")
	}

	node, exists := c.inventory.Node(NodeID(site.Attach))
	if !exists {
		return fmt.Errorf("attach node not found in inventory")
	}

	topologyNode, exists := c.topology.GetNodeByID(NodeID(site.Attach))
	if !exists {
		return fmt.Errorf("attach node not found in topology")
	}

	// // Register for SID
	// dt46SID, err := c.serviceRegistry.GetOrGenerateSID(topologyNode.Locator, fmt.Sprintf("%d", tenant.ID), dataplane.EncapTypeDT46)
	// if err != nil {
	// 	return fmt.Errorf("failed to generate random SID: %w", err)
	// }

	peIntent := &dataplane.PE_PortalIntent{
		HostFacingInterface: "eth1",
		TunnelInterfaceID:   req.IfID,
		SitePrefix:          site.Prefix,
		TenantPrefix:        tenant.Allocation,
	}

	err := c.serviceRegistry.UpsertPEIntentForNode(
		ctx, string(node.ID), fmt.Sprintf("%d", tenant.ID), req.PortalID, peIntent, topologyNode.Locator,
	)
	if err != nil {
		return fmt.Errorf("failed to set intent for tenant: %w", err)
	}

	return nil
}

func (c *Controller) startSnapshotPublisher(ctx context.Context) error {
	publisher, err := statekv.NewPublisherFor(ctx, c.js, statekv.ControlState)
	if err != nil {
		return fmt.Errorf("create control snapshot publisher: %w", err)
	}

	snapshots := NewSnapshotPublisher(
		publisher,
		c.config.SnapshotInterval,
		c.logger.With(log.Domain(log.DomainControlPlaneLifecycle)),
		c.topology.Graph(),
		c.topology.GetDomainMetadata(),
		c.inventory,
		c.serviceRegistry,
	)

	go snapshots.Run(ctx)

	return nil
}

type PCEUpdateSiteCombination struct {
	Local, Remote *Site
	Paths         map[CostDimension]*Path
}

func (c *Controller) startPCEUpdatesDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case paths := <-c.pceUpdates:
			// Convert NodeID->NodeID->CostDimension->NodeID lists to
			// ,for each PENode and its tenants, [prefix][]netip.Addr
			// We need to get the egress PE's End.DT46 for the tenant as well
			//
			// This will construct all the necessary outgoing customer sites
			// to remote PE. It will basically aggregate all the (site,site)
			// combination generated by the inner loop for a this PE/P node.
			tenantPathCombinationByNode := make(map[NodeID]map[int][]PCEUpdateSiteCombination)
			for nodeID, byNodePaths := range paths {
				tenantPathCombinationByNode[nodeID] = make(map[int][]PCEUpdateSiteCombination)
				// Get all tenants from that node
				_, exists := c.inventory.Node(nodeID)
				if !exists {
					c.logger.ErrorContext(ctx, "found node without a inventory entry", log.NodeID(string(nodeID)))
					continue
				}

				// Get all tenants in localNode
				localSites := c.tenants.SitesByPop(string(nodeID))
				localSitesByTenant := make(map[int][]*Site)
				for _, site := range localSites {
					localSitesByTenant[site.TenantID] = append(localSitesByTenant[site.TenantID], site)
				}

				// This section constructs the (site,site) for the same tenant between
				// (local, remote). The outer loop will aggregate the final (site,site)
				// for all combinatiosn originating out of localNode (nodeID)
				for remoteNodeID, paths := range byNodePaths {
					if nodeID == remoteNodeID {
						continue
					}

					remoteSites := c.tenants.SitesByPop(string(remoteNodeID))

					for _, remoteSite := range remoteSites {
						localSitesOfTenant, exists := localSitesByTenant[remoteSite.TenantID]
						if !exists {
							// This site of Tenant does not have a site in localNode
							continue
						}

						// A PE can hold more than one tenant site. Remember that!
						// So if localSite contains 2 remote site
						// Two routes have to be installed: one for siteA and another for siteB
						// Both at the PE. For now, we will use the SitePrefix as the prefix
						// However, in the future, based on user input, this site prefix
						// can be divided into subnets with different policies
						for _, localSite := range localSitesOfTenant {
							tenantPathCombinationByNode[nodeID][localSite.TenantID] = append(
								tenantPathCombinationByNode[nodeID][localSite.TenantID],
								PCEUpdateSiteCombination{
									Local:  localSite,
									Remote: remoteSite,
									Paths:  paths,
								},
							)
						}
					}
					// c.logger.InfoContext(
					// 	ctx,
					// 	fmt.Sprintf("tenant Combination for %s -> %s", nodeID, remoteNodeID),
					// 	slog.Any("combination", someCombinationList),
					// )
				}

				c.logger.InfoContext(
					ctx,
					fmt.Sprintf("all site-site combination from Node %s", nodeID),
					slog.Any("combination", tenantPathCombinationByNode[nodeID]),
				)
			}

			for nodeID, tenantsMap := range tenantPathCombinationByNode {
				localNode, exists := c.topology.GetNodeByID(nodeID)
				if !exists {
					c.logger.ErrorContext(ctx, "failed to get local node entry", log.NodeID(string(nodeID)))
					continue
				}

				for tenantID, siteComb := range tenantsMap {
					tenantSIDIntents := make([]dataplane.PESIDInstallIntent, 0)

					for _, comb := range siteComb {

						remoteNode, exists := c.topology.GetNodeByID(NodeID(comb.Remote.Attach))
						if !exists {
							c.logger.ErrorContext(ctx, "failed to get Node entry", log.NodeID(string(nodeID)))
							continue
						}

						dt46, err := c.serviceRegistry.getOrGenerateSID(
							remoteNode.Locator,
							fmt.Sprintf("%d", tenantID),
							dataplane.EncapTypeDT46,
						)

						if err != nil {
							c.logger.ErrorContext(ctx, "failed to get/generated DT46 SID for tenant @ remote", slog.Int("tenant", tenantID), log.NodeID(string(remoteNode.ID)))
							continue
						}

						sidAddrList := make([]netip.Addr, 0)

						// Convert A>B>C -> fc00:0:1::,fc00:0:2::,fc00:0:3::
						for dim, paths := range comb.Paths {
							// TODO: Support multiple dimensions later
							if dim != COSTDIM_LATENCY {
								continue
							}

							for _, p := range paths.Nodes {
								if p == NodeID(comb.Local.Attach) {
									continue
								}

								if p == NodeID(comb.Remote.Attach) {
									// The last SID will be a DT46 SID, not the End SID of the remote node.
									continue
								}

								node, exists := c.topology.GetNodeByID(p)
								if !exists {
									c.logger.ErrorContext(ctx, "failed to get Node entry inside inner loop", log.NodeID(string(nodeID)))
									continue
								}

								sidAddrList = append(sidAddrList, node.Locator.Addr())
							}
						}

						sidAddrList = append(sidAddrList, dt46)
						tenantSIDIntents = append(tenantSIDIntents, dataplane.PESIDInstallIntent{
							TenantID:     fmt.Sprint(tenantID),
							PrefixRoutes: []netip.Prefix{comb.Remote.Prefix},
							Segments:     sidAddrList,
							Color:        0,
						})
					}

					err := c.serviceRegistry.UpsertSIDSegsForTenantOnNode(
						ctx, fmt.Sprint(tenantID), string(nodeID), localNode.Locator, tenantSIDIntents,
					)

					if err != nil {
						c.logger.ErrorContext(ctx, "failed to upsert tenant intents to service registry", log.Err(err))
						continue
					}
				}
			}
		}
	}
}
