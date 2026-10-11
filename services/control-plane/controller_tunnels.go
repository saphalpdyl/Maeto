package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/saphalpdyl/maeto/libs/controlapi"
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
			c.logger.ErrorContext(ctx, "couldn't find tenant before assinging CPEIntent", slog.Int("tenant_id", int(site.TenantID)))
			return
		}

		// Create the intent if not exist
		c.serviceRegistry.mu.Lock()
		_, exists = c.serviceRegistry.registry[req.PortalID]
		if !exists {
			cpeIntent := nodesync.CPEIntent{
				TunnelInterfaceID:    0, // empty intent, 0 means no tunnel yet
				TunnelPE:             site.AttachNode,
				TunnelPEEndpointAddr: attachNode.Access.Address.Addr(),
				TenantID:             fmt.Sprintf("%d", site.TenantID),
				TenantPrefix:         tenant.Allocation,
				SitePrefix:           site.Prefix,
			}

			c.serviceRegistry.registry[req.PortalID] = &nodesync.NodeIntent{
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

	attachNode, exists := c.inventory.Node(NodeID(site.Attach))
	if !exists {
		return fmt.Errorf("attach node not found in inventory")
	}

	cpeIntent := nodesync.CPEIntent{
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

	portalIntent := &nodesync.PE_PortalIntent{
		HostFacingInterface: "eth1",
		TunnelInterfaceID:   req.IfID,
		SitePrefix:          site.Prefix,
		TenantPrefix:        tenant.Allocation,
	}

	err := c.serviceRegistry.UpsertPEIntentForNode(
		ctx, string(node.ID), fmt.Sprintf("%d", tenant.ID), req.PortalID, portalIntent, topologyNode.Locator,
	)
	if err != nil {
		return fmt.Errorf("failed to set intent for tenant: %w", err)
	}

	return nil
}
