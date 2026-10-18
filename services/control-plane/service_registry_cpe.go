package controlplane

import (
	"context"
	"fmt"
	"time"

	"github.com/saphalpdyl/maeto/libs/nodesync"
)

func (r *ServiceRegistry) getOrCreateRegistryEntryForCPE(portalID string) *nodesync.NodeIntent {
	current, exists := r.registry[portalID]
	if !exists {
		current = &nodesync.NodeIntent{
			NodeType:   nodesync.NodeTypeCPE,
			Intent:     &nodesync.CPEIntent{PortalID: portalID},
			Timestamp:  time.Now(),
			Generation: 0,
			Version:    1,
		}
		r.registry[portalID] = current
	}

	return current
}

func (r *ServiceRegistry) UpsertCPEIntentForSite(ctx context.Context, tenantID string, portalID string, cpe *nodesync.CPEIntent) error {
	r.mu.Lock()
	current := r.getOrCreateRegistryEntryForCPE(portalID)

	current.Intent = cpe
	current.Generation++
	current.Timestamp = time.Now()

	snapshot := current.Clone()

	r.mu.Unlock()

	if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixCPE, portalID, nodesync.LaneIntent), snapshot); err != nil {
		return fmt.Errorf("failed to publish intent for portalID: %s, err = %w", portalID, err)
	}

	return nil
}
