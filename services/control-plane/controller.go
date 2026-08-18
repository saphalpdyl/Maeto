package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	log "github.com/saphalpdyl/maeto/services/control-plane/log"
)

type Controller struct {
	config Config
	logger *slog.Logger
	js     jetstream.JetStream

	intentPublisher *IntentPublisher

	topology  *ClabTopologyManager
	customers CustomerRepository
	registry  *ServiceRegistry
	pce       *PCE

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

		intentPublisher: intentPublisher,

		topology:  topology,
		customers: customers,
		registry:  NewServiceRegistry(&ServiceRegistryConfig{}),
		pce:       NewPCE(),

		ready: false,
	}, nil
}

func (c *Controller) Start(ctx context.Context) {
	c.logger.InfoContext(ctx, "controller starting", log.ListenAddress(c.config.ListenAddress))

	go c.pce.Run(ctx, c.topology.Graph())

	if err := c.setupHealthEndpoint(ctx); err != nil {
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
