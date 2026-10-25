package controlplane

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
)

// Must be done under a lock
func (r *ServiceRegistry) getOrCreateRegistryEntryForPE(nodeID string) *nodesync.NodeIntent {
	current, exists := r.registry[nodeID]
	if !exists {
		r.registry[nodeID] = &nodesync.NodeIntent{
			NodeType: nodesync.NodeTypePE,
			Intent: &nodesync.PEIntent{
				NodeID:             nodeID,
				Tenants:            make(map[string]*nodesync.TenantIntent),
				Peers:              make(map[string]nodesync.PeerIntent),
				TrafficClassIntent: nil,
			},
			Timestamp:  time.Now(),
			Generation: 1,
			Version:    1,
		}

		current = r.registry[nodeID]
	}

	return current
}

func (r *ServiceRegistry) getOrCreateTenantIntentForPE(
	ctx context.Context,
	tenantID string,
	locatorPrefix netip.Prefix,
	pe *nodesync.PEIntent,
) (*nodesync.TenantIntent, error) {
	tenantIntent, exists := pe.Tenants[tenantID]
	if exists && tenantIntent != nil {
		return tenantIntent, nil
	}

	dt46SID, err := r.getOrGenerateSID(ctx, locatorPrefix, tenantID, dataplane.EncapTypeDT46)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random SID: %w", err)
	}

	tenantIntent = &nodesync.TenantIntent{
		PortalIntents: make(map[string]nodesync.PE_PortalIntent),
		DT46SID:       dt46SID,
	}
	pe.Tenants[tenantID] = tenantIntent

	return tenantIntent, nil
}

func (r *ServiceRegistry) StageSIDSegsForTenantOnNode(
	ctx context.Context,
	tx *RegistryTx,
	tenantID string,
	localNodeID NodeID,
	locatorPrefix netip.Prefix,
	pathsInstallIntents []nodesync.PESIDInstallIntent,
) {
	tx.MutatePE(localNodeID, func(intent *nodesync.PEIntent) error {
		tenantIntent, err := r.getOrCreateTenantIntentForPE(ctx, tenantID, locatorPrefix, intent)
		if err != nil {
			return fmt.Errorf("failed to get or create tenant intent for pe %s: %w", localNodeID, err)
		}

		tenantIntent.InstallPaths = pathsInstallIntents
		return nil
	})
}

func (r *ServiceRegistry) StagePortalIntentForNode(ctx context.Context, tx *RegistryTx, nodeID NodeID, tenantID string, portalID string, portal *nodesync.PE_PortalIntent, locatorPrefix netip.Prefix) {
	tx.MutatePE(nodeID, func(intent *nodesync.PEIntent) error {
		tenantIntent, err := r.getOrCreateTenantIntentForPE(ctx, tenantID, locatorPrefix, intent)
		if err != nil {
			return fmt.Errorf("failed to get or create tenant intent for pe %s: %w", nodeID, err)
		}

		tenantIntent.PortalIntents[portalID] = *portal
		return nil
	})
}

func (r *ServiceRegistry) StagePeersForPE(
	tx *RegistryTx,
	nodeID NodeID,
	peers map[string]nodesync.PeerIntent,
) {
	tx.MutatePE(nodeID, func(intent *nodesync.PEIntent) error {
		intent.Peers = peers
		return nil
	})
}

func (r *ServiceRegistry) StageTrafficClassMappingForPE(
	tx *RegistryTx,
	nodeID NodeID,
	mapping []nodesync.PETrafficClassIntent,
) {
	tx.MutatePE(nodeID, func(intent *nodesync.PEIntent) error {
		intent.TrafficClassIntent = mapping
		return nil
	})
}
