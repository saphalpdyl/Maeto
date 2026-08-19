// service_registry holds information about desired FIB states
// We will try our best to not include detailed Linux-specific implementation details here,
// so that we can eventually move this to a separate package and share it with the agent.
package controlplane

import (
	"context"
	"fmt"
	"sync"
)

type ServiceRegistryConfig struct {
}

type ServiceRegistry struct {
	config   *ServiceRegistryConfig
	registry map[NodeID]*NodeIntent

	publisher *IntentPublisher

	mu sync.RWMutex // guards desired state
}

type NodeIntent struct {
	NodeID        NodeID          `json:"node_id"`
	TenantIntents map[int]*Intent `json:"tenant_intents"`
}

type Intent struct {
	Gen   uint64 `json:"gen"`
	Sites []Site `json:"sites"`
}

func NewServiceRegistry(config *ServiceRegistryConfig, intentPublisher *IntentPublisher) *ServiceRegistry {
	return &ServiceRegistry{
		config:    config,
		registry:  make(map[NodeID]*NodeIntent),
		publisher: intentPublisher,
	}
}

func (r *ServiceRegistry) SetIntentForTenant(ctx context.Context, node NodeID, tenantID int, intent *Intent) error {
	r.mu.Lock()

	if _, exists := r.registry[node]; !exists {
		r.registry[node] = &NodeIntent{
			NodeID:        node,
			TenantIntents: make(map[int]*Intent),
		}
	}

	r.registry[node].TenantIntents[tenantID] = intent
	r.mu.Unlock()

	r.mu.RLock()
	defer r.mu.RUnlock()

	_, err := r.publisher.Publish(ctx, node, *r.registry[node])

	return err
}

// Restore recreates the intent bucket and republishes every node's intent.
func (r *ServiceRegistry) Restore(ctx context.Context) error {
	if err := r.publisher.Ensure(ctx); err != nil {
		return err
	}

	r.mu.RLock()
	intents := make([]NodeIntent, 0, len(r.registry))
	for _, intent := range r.registry {
		intents = append(intents, *intent)
	}
	r.mu.RUnlock()

	for _, intent := range intents {
		if _, err := r.publisher.Publish(ctx, intent.NodeID, intent); err != nil {
			return fmt.Errorf("republish intent for %s: %w", intent.NodeID, err)
		}
	}

	return nil
}
