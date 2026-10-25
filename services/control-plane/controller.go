package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/libs/store"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	log "github.com/saphalpdyl/maeto/services/control-plane/log"
)

type Controller struct {
	config Config
	logger *slog.Logger
	js     jetstream.JetStream

	topology            *ClabTopologyManager
	inventory           NodeInventory
	tenants             TenantRepository
	serviceRegistry     *ServiceRegistry
	pce                 *PCE
	costGraph           *CostGraph
	telemetryCollectors map[NodeID]*TelemetryCollector
	topologyReports     *TopologyReportCollector
	db                  *store.DataStore

	ready bool

	pceUpdates chan PathSet
}

func NewController(
	ctx context.Context,
	js jetstream.JetStream,
	db *store.DataStore,
	config Config,
	logger *slog.Logger,
) (*Controller, error) {
	// Set up data store if not set
	controlState, err := db.Queries.GetControlState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get control state: %w", err)
	}

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

	costGraph, err := NewTestCostGraph(topology.Graph())
	if err != nil {
		return nil, fmt.Errorf("failed to create cost graph: %w", err)
	}

	// Telemetry collectors
	// inventory is more authoritative than topology
	telemetryCollectors := make(map[NodeID]*TelemetryCollector)
	for _, n := range inventory.Nodes() {
		telemetryCollectors[n.ID] = NewTelemetryCollector(
			js,
			logger.With(log.Domain(log.DomainTelemetryCollection)),
			n.ID,
			fmt.Sprintf("%s.%s", probe.ProbeResultSubjectPrefix, string(n.ID)),
			inventory,
			topology,
			costGraph,
		)
	}

	sidAllocationMap := make(map[netip.Addr]bool)
	sidTenantMap := make(map[string]netip.Addr)
	sidCursor := uint16(controlState.SidCursor)

	allocations, err := db.Queries.ListSIDAllocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocation: %w", err)
	}

	for _, al := range allocations {
		tenantMapKey := fmt.Sprintf(
			"%s.%s.%s",
			al.Locator.Addr().String(),
			al.TenantID,
			al.SidType,
		)

		sidAllocationMap[al.Sid] = true
		sidTenantMap[tenantMapKey] = al.Sid
	}

	// Intent KV
	intentPublisher, err := nodesync.NewPublisher(ctx, js, nodesync.IntentBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to create intent publisher: %w", err)
	}

	sr := NewServiceRegistry(
		&ServiceRegistryConfig{},
		intentPublisher,
		db,
		sidCursor,
		sidAllocationMap,
		sidTenantMap,
		logger.With(log.Domain(log.DomainServiceRegistry)),
	)

	pceUpdatesChan := make(chan PathSet, 32)

	return &Controller{
		config: config,
		logger: logger,
		js:     js,

		topology:            topology,
		inventory:           inventory,
		tenants:             tenants,
		serviceRegistry:     sr,
		telemetryCollectors: telemetryCollectors,
		topologyReports: NewTopologyReportCollector(
			js,
			logger.With(log.Domain(log.DomainControlPlaneLifecycle)),
			topology,
		),
		costGraph: costGraph,
		pce: NewPCE(
			costGraph,
			tenants,
			pceUpdatesChan,
			logger.With(log.Domain(log.DomainPCE)),
		),
		pceUpdates: pceUpdatesChan,
		db:         db,

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
	go c.topologyReports.Run(ctx)

	if err := c.startSnapshotPublisher(ctx); err != nil {
		c.logger.ErrorContext(ctx, "failed to start snapshot publisher",
			log.Domain(log.DomainControlPlaneLifecycle),
			log.Err(err),
		)
	}

	tx := c.serviceRegistry.BeginTx()
	c.stagePeerIntents(tx, ctx)
	c.stageTrafficClassMapping(tx)
	err := tx.Commit(ctx)

	if err != nil {
		c.logger.ErrorContext(ctx, "failed to commit transaction", log.Err(err))
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

	c.startTelemetryCollection(ctx)

	c.startDebugTools(
		ctx,
		c.logger.With(log.Domain(log.DomainDebugTools)),
	)

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

func (c *Controller) startSnapshotPublisher(ctx context.Context) error {
	publisher, err := nodesync.NewPublisher(ctx, c.js, nodesync.PaneControlBucket)
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
		c.tenants,
		c.pce.PathStore,
		c.costGraph,
		c.pce.Changes,
		&c.pce.bandwidthReservationReg,
	)

	go snapshots.Run(ctx)

	return nil
}
