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

func (r *ServiceRegistry) UpsertPEIntentForNode(ctx context.Context, nodeID string, tenantID string, portalID string, intent *dataplane.PE_PortalIntent) error {
	r.mu.Lock()

	current, exists := r.registry[nodeID]
	if !exists {
		r.registry[nodeID] = &dataplane.NodeIntent{
			NodeType: dataplane.NodeTypePE,
			Intent: &dataplane.PEIntent{
				NodeID:  nodeID,
				Tenants: make(map[string]map[string]dataplane.PE_PortalIntent),
			},
			Timestamp:  time.Now(),
			Generation: 1,
			Version:    1,
		}

		current = r.registry[nodeID]
	}

	peIntent, ok := current.Intent.(*dataplane.PEIntent)
	if !ok {
		r.logger.ErrorContext(ctx, "failed to cast Intent to PEIntent")
		return fmt.Errorf("failed to cast Intent to PEIntent")
	}

	tenantIntent, exists := peIntent.Tenants[tenantID]
	if !exists {
		peIntent.Tenants[tenantID] = make(map[string]dataplane.PE_PortalIntent)
		peIntent.Tenants[tenantID][portalID] = *intent
	} else {
		tenantIntent[portalID] = *intent
	}

	current.Timestamp = time.Now()
	current.Generation++

	snapshot := current.Clone()

	r.mu.Unlock()

	if _, err := r.publisher.Publish(ctx, intentkv.Key(intentkv.PrefixPE, nodeID), snapshot); err != nil {
		return fmt.Errorf("publish pe intent for %s: %w", nodeID, err)
	}

	return nil
}

func (r *ServiceRegistry) UpsertCPEIntentForSite(ctx context.Context, tenantID string, portalID string, intent *dataplane.CPEIntent) error {
	r.mu.Lock()
	current, exists := r.registry[portalID]
	if !exists {
		r.registry[portalID] = &dataplane.NodeIntent{
			NodeType:   dataplane.NodeTypeCPE,
			Intent:     intent,
			Timestamp:  time.Now(),
			Generation: 1,
			Version:    1,
		}
	} else {
		current.Intent = intent
		current.Generation++
		current.Timestamp = time.Now()
	}

	snapshot := current.Clone()

	r.mu.Unlock()

	if _, err := r.publisher.Publish(ctx, intentkv.Key(intentkv.PrefixCPE, portalID), snapshot); err != nil {
		return fmt.Errorf("failed to publish intent for portalID: %s, err = %w", portalID, err)
	}

	return nil
}

// Restore recreates the intent bucket and republishes every node's intent.
func (r *ServiceRegistry) Restore(ctx context.Context) error {
	if err := r.publisher.Ensure(ctx); err != nil {
		return err
	}

	r.mu.RLock()
	intents := make([]dataplane.NodeIntent, 0, len(r.registry))
	for _, intent := range r.registry {
		intents = append(intents, *intent.Clone())
	}
	r.mu.RUnlock()

	for _, intent := range intents {
		switch i := intent.Intent.(type) {
		case *dataplane.CPEIntent:
			if _, err := r.publisher.Publish(ctx, intentkv.Key(intentkv.PrefixCPE, i.PortalID), intent); err != nil {
				return fmt.Errorf("republish intent for %s: %w", i.PortalID, err)
			}
		case *dataplane.PEIntent:
			if _, err := r.publisher.Publish(ctx, intentkv.Key(intentkv.PrefixPE, i.NodeID), intent); err != nil {
				return fmt.Errorf("republish intent for %s: %w", i.NodeID, err)
			}
		}
	}

	return nil
}
