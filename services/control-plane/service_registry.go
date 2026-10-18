// service_registry holds information about desired FIB states
// We will try our best to not include detailed Linux-specific implementation details here,
// so that we can eventually move this to a separate package and share it with the agent.
package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"

	"sync"

	"github.com/saphalpdyl/maeto/libs/nodesync"
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
}

func NewServiceRegistry(config *ServiceRegistryConfig, intentPublisher *nodesync.Publisher, logger *slog.Logger) *ServiceRegistry {
	return &ServiceRegistry{
		config:           config,
		registry:         make(map[string]*nodesync.NodeIntent),
		sidAllocationMap: make(map[netip.Addr]bool),
		sidTenantMap:     make(map[string]netip.Addr),
		publisher:        intentPublisher,
		logger:           logger,
		sidCursor:        0,
	}
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
