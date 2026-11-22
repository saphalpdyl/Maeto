package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

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

		attachNode, exists := c.inventory.Node(NodeID(site.AttachNodeID))
		if !exists {
			c.logger.ErrorContext(ctx, "attach node not found in inventory", log.NodeID(site.AttachNodeID))
			return
		}

		if !attachNode.HasAccessSide() {
			c.logger.ErrorContext(ctx, "attach node has no access side", log.NodeID(site.AttachNodeID))
			return
		}

		attachments := make(map[string]controlapi.PEAttachment)
		attachments[string(attachNode.ID)] = controlapi.PEAttachment{
			NodeID:             string(attachNode.ID),
			AttachAddr:         attachNode.Access.Address.Addr().String(),
			RemoteSwanIdentity: fmt.Sprintf("%s.maeto.net", attachNode.Name),
		}

		node, exists := c.topology.GetNodeByID(NodeID(site.AttachNodeID))
		if !exists {
			c.logger.ErrorContext(ctx, "attach node not found in topology", log.NodeID(site.AttachNodeID))
			return
		}

		for _, n := range node.ProtectedBy {
			attachNode, exists = c.inventory.Node(n)
			if !exists {
				continue
			}

			if !attachNode.HasAccessSide() {
				c.logger.WarnContext(ctx, "protector node has no access side, cannot be a protector without access", log.NodeID(site.AttachNodeID))
				continue
			}

			attachments[string(attachNode.ID)] = controlapi.PEAttachment{
				NodeID:             string(attachNode.ID),
				AttachAddr:         attachNode.Access.Address.Addr().String(),
				RemoteSwanIdentity: fmt.Sprintf("%s.maeto.net", attachNode.Name),
			}
		}

		data, err := json.Marshal(controlapi.PortalAuthEndpointResponse{
			LocalSwanIdentity: site.Identity,
			Prefix:            site.Prefix.String(),
			Attachments:       attachments,
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
			c.logger.ErrorContext(ctx, "couldn't find tenant before assinging CPEIntent", slog.Int("tenant_id", int(site.TenantID)))
			return
		}

		// Create the intent if not exist
		c.serviceRegistry.mu.Lock()
		_, exists = c.serviceRegistry.registry[req.PortalID]
		if !exists {
			cpeIntent := nodesync.CPEIntent{
				Tunnels:      make([]nodesync.CPETunnelIntent, 0), // Node tunnels
				TenantID:     fmt.Sprintf("%d", site.TenantID),
				TenantPrefix: tenant.Allocation,
				SitePrefix:   site.Prefix,
			}

			c.serviceRegistry.registry[req.PortalID] = &nodesync.NodeIntent{
				ID:         req.PortalID,
				NodeType:   nodesync.NodeTypeCPE,
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

	attachNode, exists := c.topology.GetNodeByID(NodeID(site.AttachNodeID))
	if !exists {
		return fmt.Errorf("attach node not found in topology")
	}

	tunnels := make([]nodesync.CPETunnelIntent, 0)
	for remoteID, ifID := range req.IfIds {
		// Derive the node from remoteID
		nodeName, found := strings.CutSuffix(remoteID, ".maeto.net")
		if !found {
			c.logger.WarnContext(ctx, "invalid attach node name")
			continue
		}

		node, exists := c.inventory.NodeByName(nodeName)
		if !exists {
			c.logger.WarnContext(ctx, "invalid attach node name")
			continue
		}

		if attachNode.ID != node.ID && !slices.Contains(attachNode.ProtectedBy, node.ID) {
			c.logger.WarnContext(ctx, "node not allowed to create tunnels for this cpe", log.NodeID(string(node.ID)), "portal_id", req.PortalID)
			continue
		}

		role := nodesync.TunnelRoleStandby
		if site.AttachNodeID == string(node.ID) {
			role = nodesync.TunnelRolePrimary
		}

		tunnels = append(tunnels, nodesync.CPETunnelIntent{
			TunnelInterfaceID:    ifID,
			TunnelPE:             string(node.ID),
			TunnelPEEndpointAddr: node.Access.Address.Addr(),
			Role:                 role,
		})
	}

	cpeIntent := nodesync.CPEIntent{
		TenantID:     fmt.Sprintf("%d", site.TenantID),
		TenantPrefix: tenant.Allocation,
		SitePrefix:   site.Prefix,
		PortalID:     req.PortalID,
		Tunnels:      tunnels,
		DownNodes:    c.topology.Graph().DownNodes(),
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

	node, exists := c.inventory.Node(NodeID(req.NodeID))
	if !exists {
		return fmt.Errorf("attach node not found in inventory")
	}

	topologyNode, exists := c.topology.GetNodeByID(NodeID(req.NodeID))
	if !exists {
		return fmt.Errorf("attach node not found in topology")
	}

	// Primary node
	attachNode, exists := c.topology.GetNodeByID(NodeID(site.AttachNodeID))
	if !exists {
		return fmt.Errorf("site attach node %s not found in topology", site.AttachNodeID)
	}

	if attachNode.ID != topologyNode.ID && !slices.Contains(attachNode.ProtectedBy, topologyNode.ID) {
		return fmt.Errorf("node %s is neither the attach node nor a protector for portal %s", req.NodeID, req.PortalID)
	}

	role := nodesync.PERoleStandby
	protectedInfo := nodesync.ProtectedInfo{}

	if attachNode.ID == topologyNode.ID {
		role = nodesync.PERolePrimary
	} else {
		var err error
		protectedInfo.DT46SID, err = c.serviceRegistry.GetOrGenerateSID(
			ctx,
			attachNode.Locator,
			fmt.Sprintf("%d", site.TenantID),
			dataplane.EncapTypeDT46,
		)

		if err != nil {
			c.logger.ErrorContext(ctx, "failed to get or generate SID for tenant during replication", log.Err(err))
			return fmt.Errorf("failed to get or generate SID for tenant during replication: %v", err)
		}

		protectedInfo.PrimaryNodeID = site.AttachNodeID
	}

	reservationBandwidth := make(map[uint64]float64)
	maps.Copy(reservationBandwidth, site.ClassMbps)

	portalIntent := &nodesync.PE_PortalIntent{
		HostFacingInterface: "eth1",
		TunnelInterfaceID:   req.IfID,
		SitePrefix:          site.Prefix,
		TenantPrefix:        tenant.Allocation,
		ReservedBandwidth:   reservationBandwidth,
		Role:                role,
		ProtectedInfo:       protectedInfo,
	}

	// Backup SR Routes
	allSitesOfTenant := c.tenants.SitesByTenantID(site.TenantID)
	egressSites := make([]nodesync.EgressSiteIntent, 0)
	for _, s := range allSitesOfTenant {
		// Don't add backup routes for sites belonging to the PE
		// This does not include backup sites
		if s.AttachNodeID == string(node.ID) {
			continue
		}

		attachNode, exists := c.topology.GetNodeByID(NodeID(s.AttachNodeID))
		if !exists {
			c.logger.WarnContext(
				ctx,
				"failed to find attach node in topology when calculating backup routes",
				"attach_node",
				s.AttachNodeID,
				"portal_id",
				s.PortalID,
			)
			continue
		}

		dt46, err := c.serviceRegistry.GetOrGenerateSID(
			ctx,
			attachNode.Locator,
			fmt.Sprintf("%d", s.TenantID),
			dataplane.EncapTypeDT46,
		)
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to get or generate SID for tenant during replication", log.Err(err))
			continue
		}

		egressSites = append(egressSites, nodesync.EgressSiteIntent{
			Prefix:  s.Prefix,
			DT46SID: dt46,
		})
	}

	tx := c.serviceRegistry.BeginTx()
	c.serviceRegistry.StagePortalIntentForNode(ctx, tx, node.ID, fmt.Sprintf("%d", tenant.ID), req.PortalID, portalIntent, topologyNode.Locator)
	c.serviceRegistry.StageEgressBackupSRRoutes(ctx, tx, node.ID, fmt.Sprintf("%d", tenant.ID), topologyNode.Locator, egressSites)
	c.buildAndStageStandbyActivation(ctx, c.topology.graph, tx)

	err := tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to set intent for tenant: %w", err)
	}

	return nil
}

func (c *Controller) expectedResyncReplies() int {
	expected := 0

	for _, node := range c.inventory.Nodes() {
		if node.HasAccessSide() {
			expected++
		}
	}

	for _, tenant := range c.tenants.Tenants() {
		expected += len(tenant.Sites)
	}

	return expected
}

func (c *Controller) sendTunnelResyncRequest(ctx context.Context, timeout time.Duration) error {
	nc := c.js.Conn()

	inbox := nats.NewInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return fmt.Errorf("subscribe to resync inbox: %w", err)
	}
	defer sub.Unsubscribe() // nolint:errcheck

	if err := nc.PublishRequest(controlapi.SubjectControllerPETunnelResync, inbox, nil); err != nil {
		return fmt.Errorf("publish pe tunnel resync: %w", err)
	}

	if err := nc.PublishRequest(controlapi.SubjectControllerCPETunnelResync, inbox, nil); err != nil {
		return fmt.Errorf("publish cpe tunnel resync: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	expected := c.expectedResyncReplies()
	replies := 0

	for replies < expected {
		if _, err := sub.NextMsgWithContext(waitCtx); err != nil {
			break
		}

		replies++
	}

	c.logger.InfoContext(ctx, "tunnel resync finished",
		slog.Int("replies", replies),
		slog.Int("expected", expected),
	)

	return nil
}
