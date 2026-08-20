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
	Gen   uint64          `json:"gen"`
	Sites map[string]Site `json:"sites"`
}

func (n *NodeIntent) clone() NodeIntent {
	out := NodeIntent{NodeID: n.NodeID, TenantIntents: make(map[int]*Intent, len(n.TenantIntents))}
	for tenantID, intent := range n.TenantIntents {
		sites := make(map[string]Site, len(intent.Sites))
		for portalID, site := range intent.Sites {
			sites[portalID] = site
		}
		out.TenantIntents[tenantID] = &Intent{Gen: intent.Gen, Sites: sites}
	}

	return out
}

func NewServiceRegistry(config *ServiceRegistryConfig, intentPublisher *IntentPublisher) *ServiceRegistry {
	return &ServiceRegistry{
		config:    config,
		registry:  make(map[NodeID]*NodeIntent),
		publisher: intentPublisher,
	}
}

func (r *ServiceRegistry) UpsertSite(ctx context.Context, node NodeID, tenantID int, site Site) error {
	r.mu.Lock()

	nodeIntent, ok := r.registry[node]
	if !ok {
		nodeIntent = &NodeIntent{NodeID: node, TenantIntents: make(map[int]*Intent)}
		r.registry[node] = nodeIntent
	}

	intent, ok := nodeIntent.TenantIntents[tenantID]
	if !ok {
		intent = &Intent{Sites: make(map[string]Site)}
		nodeIntent.TenantIntents[tenantID] = intent
	}

	intent.Sites[site.PortalID] = site
	intent.Gen++

	snapshot := nodeIntent.clone()
	r.mu.Unlock()

	_, err := r.publisher.Publish(ctx, node, snapshot)

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
		intents = append(intents, intent.clone())
	}
	r.mu.RUnlock()

	for _, intent := range intents {
		if _, err := r.publisher.Publish(ctx, intent.NodeID, intent); err != nil {
			return fmt.Errorf("republish intent for %s: %w", intent.NodeID, err)
		}
	}

	return nil
}
