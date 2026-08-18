package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/saphalpdyl/maeto/libs/controlapi"
	log "github.com/saphalpdyl/maeto/services/control-plane/log"
)

type Controller struct {
	config Config
	logger *slog.Logger
	js     jetstream.JetStream

	topology        *ClabTopologyManager
	inventory       NodeInventory
	customers       CustomerRepository
	serviceRegistry *ServiceRegistry
	pce             *PCE

	ready bool
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

	customers := NewJSONCustomerRepository(CustomerRepositoryConfig{
		StatePath:       config.StatePath,
		TopologyDirPath: config.DataDir,
	})
	if err := customers.Load(ctx); err != nil {
		return nil, fmt.Errorf("failed to load customers: %w", err)
	}

	logger.InfoContext(ctx, "controller initialized",
		slog.Int("customers", len(customers.Customers())),
	)

	// Intent KV
	intentPublisher, err := NewIntentPublisher(ctx, js)
	if err != nil {
		return nil, fmt.Errorf("failed to create intent publisher: %w", err)
	}

	return &Controller{
		config: config,
		logger: logger,
		js:     js,

		topology:        topology,
		inventory:       inventory,
		customers:       customers,
		serviceRegistry: NewServiceRegistry(&ServiceRegistryConfig{}, intentPublisher),
		pce:             NewPCE(),

		ready: false,
	}, nil
}

func (c *Controller) Start(ctx context.Context) {
	c.logger.InfoContext(ctx, "controller starting", log.ListenAddress(c.config.ListenAddress))

	// Load intents for PoP
	c.serviceRegistry.mu.Lock()
	for _, node := range c.inventory.Nodes() {
		c.serviceRegistry.registry[node.ID] = &NodeIntent{
			NodeID:               node.ID,
			CustomerBasedIntents: make(map[int]*Intent),
		}
	}
	c.serviceRegistry.mu.Unlock()

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

	if err := c.setupHealthEndpoint(ctx); err != nil {
		return
	}

	if err := c.setupPortalAuthEndpoint(ctx); err != nil {
		return
	}

	if err := c.setupPushTunnelInitiate(ctx); err != nil {
		return
	}

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

		site, exists := c.customers.SiteByPortalID(req.PortalID)
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
		}
	})

	if err != nil {
		c.logger.ErrorContext(ctx, "failed to subscribe to maeto.control.portal.auth.identity", log.Err(err))
		return err
	}

	return nil
}

func (c *Controller) setupPushTunnelInitiate(ctx context.Context) error {
	_, err := c.js.Conn().Subscribe(controlapi.SubjectPushTunnelInitiate, func(msg *nats.Msg) {
		var req controlapi.PushTunnelInitiateRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			c.logger.ErrorContext(ctx, "failed to unmarshal request", log.Err(err))
			return
		}

		// Get customer
		site, exists := c.customers.SiteByPortalID(req.PortalID)
		if !exists {
			c.logger.ErrorContext(ctx, "portal identity not found")
			errResponse, err := json.Marshal(controlapi.PushTunnelInitiateResponse{Ok: false})
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to marshal response", log.Err(err))
				return
			}
			err = msg.Respond(errResponse)
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to send response", log.Err(err))
			}
			return
		}

		customer, exists := c.customers.Customer(site.CustomerID)
		if !exists {
			c.logger.ErrorContext(ctx, "customer not found")
			errResponse, err := json.Marshal(controlapi.PushTunnelInitiateResponse{Ok: false})
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to marshal response", log.Err(err))
				return
			}
			err = msg.Respond(errResponse)
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to send response", log.Err(err))
			}
			return
		}

		siteCopy := *site
		siteCopy.IfID = req.IfID

		err := c.serviceRegistry.SetIntentForCustomer(ctx, NodeID(req.NodeID), customer.ID, &Intent{
			Gen:   1,
			Sites: []Site{siteCopy},
		})
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to set intent for customer", log.Err(err))
			errResponse, err := json.Marshal(controlapi.PushTunnelInitiateResponse{Ok: false})
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to marshal response", log.Err(err))
				return
			}
			err = msg.Respond(errResponse)
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to send response", log.Err(err))
			}
		}

		successResponse, err := json.Marshal(controlapi.PushTunnelInitiateResponse{Ok: true})
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
