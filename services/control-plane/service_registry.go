// service_registry holds information about desired FIB states
// We will try our best to not include detailed Linux-specific implementation details here,
// so that we can eventually move this to a separate package and share it with the agent.
package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sync"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/intentkv"
)

type ServiceRegistryConfig struct {
}

type ServiceRegistry struct {
	config   *ServiceRegistryConfig
	registry map[string]*dataplane.NodeIntent

	publisher *intentkv.Publisher

	mu sync.RWMutex // guards desired state

	logger *slog.Logger
}

func NewServiceRegistry(config *ServiceRegistryConfig, intentPublisher *intentkv.Publisher, logger *slog.Logger) *ServiceRegistry {
	return &ServiceRegistry{
		config:    config,
		registry:  make(map[string]*dataplane.NodeIntent),
		publisher: intentPublisher,
		logger:    logger,
	}
}

func (r *ServiceRegistry) UpsertPEIntentForNode(ctx context.Context, nodeID string, intent *dataplane.NodeIntent, intentInner *dataplane.PEIntent) error {
	if intent.NodeType != dataplane.NodeTypePE {
		return fmt.Errorf("trying to upsert non-PE intent in UpsertPEIntent")
	}

	current, exists := r.registry[nodeID]
	if !exists {
		r.registry[nodeID] = &dataplane.NodeIntent{
			NodeType:   dataplane.NodeTypePE,
			Intent:     nil,
			Timestamp:  time.Now(),
			Generation: 1,
			Version:    1,
		}

		current = r.registry[nodeID]
	}

	r.mu.Lock()
	current.Intent = intentInner
	current.Timestamp = time.Now()
	current.Generation++
	r.mu.Unlock()

	return nil
}

func (r *ServiceRegistry) UpsertSite(ctx context.Context, ID string, nodeType dataplane.NodeType) error {
	// r.mu.Lock()

	// nodeIntent, ok := r.registry[node]
	// if !ok {
	// 	nodeIntent = &dataplane.NodeIntent{
	// 		NodeType:   "",
	// 		Intent:     json.RawMessage{},
	// 		Timestamp:  time.Time{},
	// 		Generation: 0,
	// 		Version:    0,
	// 	}
	// 	r.registry[node] = nodeIntent
	// }

	// intent, ok := nodeIntent.TenantIntents[tenantID]
	// if !ok {
	// 	intent = &Intent{Sites: make(map[string]Site)}
	// 	nodeIntent.TenantIntents[tenantID] = intent
	// }

	// intent.Sites[site.PortalID] = site
	// intent.Gen++

	// snapshot := nodeIntent.clone()
	// r.mu.Unlock()

	// _, err := r.publisher.Publish(ctx, intentkv.Key(intentkv.PrefixPE, string(node)), snapshot)

	return nil
}

// Restore recreates the intent bucket and republishes every node's intent.
func (r *ServiceRegistry) Restore(ctx context.Context) error {
	// if err := r.publisher.Ensure(ctx); err != nil {
	// 	return err
	// }

	// r.mu.RLock()
	// intents := make([]dataplane.NodeIntent, 0, len(r.registry))
	// for _, intent := range r.registry {
	// 	intents = append(intents, intent.clone())
	// }
	// r.mu.RUnlock()

	// for _, intent := range intents {
	// 	if _, err := r.publisher.Publish(ctx, intentkv.Key(intentkv.PrefixPE, string(intent.NodeID)), intent); err != nil {
	// 		return fmt.Errorf("republish intent for %s: %w", intent.NodeID, err)
	// 	}
	// }

	return nil
}
