// service_registry holds information about desired FIB states
// We will try our best to not include detailed Linux-specific implementation details here,
// so that we can eventually move this to a separate package and share it with the agent.
package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"sync"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/libs/store"
)

type ServiceRegistryConfig struct {
}

type ServiceRegistry struct {
	config   *ServiceRegistryConfig
	registry map[string]*nodesync.NodeIntent
	// We are explicitly allowing SID specific here since, for a SRv6-based SD-WAN,
	// SIDs are irrelevant for the underlying dataplane implementation
	sidAllocationMap map[netip.Addr]bool   // used to verify against collision
	sidTenantMap     map[string]netip.Addr // one tenant has exactly <=1 VRF per PE
	sidCursor        uint16                // Sequential cursor that is used to generate the hex for SID

	publisher *nodesync.Publisher

	mu sync.RWMutex // guards desired state

	logger *slog.Logger
	db     *store.DataStore
}

func NewServiceRegistry(
	config *ServiceRegistryConfig,
	intentPublisher *nodesync.Publisher,
	db *store.DataStore,
	sidCursor uint16,
	logger *slog.Logger,
) *ServiceRegistry {
	return &ServiceRegistry{
		config:           config,
		registry:         make(map[string]*nodesync.NodeIntent),
		sidAllocationMap: make(map[netip.Addr]bool),
		sidTenantMap:     make(map[string]netip.Addr),
		publisher:        intentPublisher,
		logger:           logger,
		sidCursor:        sidCursor,
		db:               db,
	}
}

func (r *ServiceRegistry) LoadPreviousStateFromDB(ctx context.Context) error {
	generations, err := r.db.Queries.ListNodeGenerations(ctx)
	if err != nil {
		return err
	}

	// Restore allocations
	allocations, err := r.db.Queries.ListSIDAllocations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get allocation: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, g := range generations {
		var intent nodesync.Intent

		if nodesync.NodeType(g.NodeType) == nodesync.NodeTypePE {
			intent = &nodesync.PEIntent{
				NodeID:             g.NodeID,
				Tenants:            make(map[string]*nodesync.TenantIntent),
				Peers:              make(map[string]nodesync.PeerIntent),
				TrafficClassIntent: make([]nodesync.PETrafficClassIntent, 0),
			}
		} else if nodesync.NodeType(g.NodeType) == nodesync.NodeTypeCPE {
			intent = &nodesync.CPEIntent{
				PortalID: g.NodeID,
			}
		} else {
			r.logger.WarnContext(ctx, "got node_generation.node_type with unknown node_type", "node_type", g.NodeType)
			continue
		}

		ni := nodesync.NodeIntent{
			ID:         g.NodeID,
			NodeType:   nodesync.NodeType(g.NodeType),
			Intent:     intent,
			Timestamp:  time.Now(),
			Generation: 0,
			Version:    uint32(g.Version + 1),
		}

		r.registry[g.NodeID] = &ni

	}

	for _, al := range allocations {
		tenantMapKey := fmt.Sprintf(
			"%s.%s.%s",
			al.Locator.Addr().String(),
			al.TenantID,
			al.SidType,
		)

		r.sidAllocationMap[al.Sid] = true
		r.sidTenantMap[tenantMapKey] = al.Sid
	}

	return nil
}

// Restore recreates the intent bucket and republishes every node's nodesync.
func (r *ServiceRegistry) Restore(ctx context.Context) error {
	if err := r.publisher.Ensure(ctx); err != nil {
		return err
	}

	r.mu.RLock()
	intents := make([]nodesync.NodeIntent, 0, len(r.registry))
	for _, ni := range r.registry {
		intents = append(intents, *ni.Clone())
	}
	r.mu.RUnlock()

	for _, ni := range intents {
		switch i := ni.Intent.(type) {
		case *nodesync.CPEIntent:
			if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixCPE, i.PortalID, nodesync.LaneIntent), ni); err != nil {
				return fmt.Errorf("republish intent for %s: %w", i.PortalID, err)
			}
		case *nodesync.PEIntent:
			if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixPE, i.NodeID, nodesync.LaneIntent), ni); err != nil {
				return fmt.Errorf("republish intent for %s: %w", i.NodeID, err)
			}
		}
	}

	return nil
}
