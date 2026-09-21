// service_registry holds information about desired FIB states
// We will try our best to not include detailed Linux-specific implementation details here,
// so that we can eventually move this to a separate package and share it with the agent.
package controlplane

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"sync"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
)

const MaxSIDCollisionChecks = 5

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

func withHextets(base netip.Prefix, offsetBits int, funcID uint16) netip.Addr {
	b := base.Addr().As16()
	offset := offsetBits / 8

	binary.BigEndian.PutUint16(b[offset:offset+2], funcID)

	return netip.AddrFrom16(b)
}

// Call under lock
func (r *ServiceRegistry) getOrGenerateSID(locatorPrefix netip.Prefix, tenantID string, sidType dataplane.EncapType) (netip.Addr, error) {
	// We don't want sticky ID across PEs
	tenantMapKey := fmt.Sprintf("%s.%s.%s", locatorPrefix.Addr().String(), tenantID, string(sidType))
	existingSID, exists := r.sidTenantMap[tenantMapKey]
	if exists {
		r.sidAllocationMap[existingSID] = true
		return existingSID, nil
	}

	for range MaxSIDCollisionChecks {
		funcID := permute(r.sidCursor)
		sid := withHextets(locatorPrefix, 48, funcID)

		allocated, exists := r.sidAllocationMap[sid]
		if !exists || !allocated {
			r.sidCursor++
			r.sidTenantMap[tenantMapKey] = sid
			r.sidAllocationMap[sid] = true
			return sid, nil
		}

		r.logger.Info("SID Collision: already is allocated", slog.String("sid", sid.String()))
		r.sidCursor++
		// TODO: uint16 might get exhausted
	}

	return netip.Addr{}, fmt.Errorf("couldn't generate SID: too many collisions, tried %d times", MaxSIDCollisionChecks)

}

func (r *ServiceRegistry) GetOrGenerateSID(locatorPrefix netip.Prefix, tenantID string, sidType dataplane.EncapType) (netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.getOrGenerateSID(locatorPrefix, tenantID, sidType)
}

// Must be done under a lock
func (r *ServiceRegistry) getOrCreateRegistryEntryForPE(nodeID string) *nodesync.NodeIntent {
	current, exists := r.registry[nodeID]
	if !exists {
		r.registry[nodeID] = &nodesync.NodeIntent{
			NodeType: nodesync.NodeTypePE,
			Intent: &nodesync.PEIntent{
				NodeID:  nodeID,
				Tenants: make(map[string]*nodesync.TenantIntent),
				Peers:   make(map[string]nodesync.PeerIntent),
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
	tenantID string,
	locatorPrefix netip.Prefix,
	pe *nodesync.PEIntent,
) (*nodesync.TenantIntent, error) {
	tenantIntent, exists := pe.Tenants[tenantID]
	if exists && tenantIntent != nil {
		return tenantIntent, nil
	}

	dt46SID, err := r.getOrGenerateSID(locatorPrefix, tenantID, dataplane.EncapTypeDT46)
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

func (r *ServiceRegistry) UpsertSIDSegsForTenantOnNode(
	ctx context.Context,
	tenantID string,
	localNodeID string,
	locatorPrefix netip.Prefix,
	pathsInstallIntents []nodesync.PESIDInstallIntent,
) error {
	r.mu.Lock()
	r.logger.InfoContext(ctx, fmt.Sprintf("got SID Install Intent for tenant %s on Node %s", tenantID, localNodeID), slog.Any("intents", pathsInstallIntents))

	current := r.getOrCreateRegistryEntryForPE(localNodeID)
	peIntent, ok := current.Intent.(*nodesync.PEIntent)
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("failed to cast NodeIntent to PEIntent")
	}

	tenantIntent, err := r.getOrCreateTenantIntentForPE(
		tenantID, locatorPrefix, peIntent,
	)
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("failed to get or create tenant intent for pe: %w", err)
	}

	tenantIntent.InstallPaths = pathsInstallIntents

	current.Timestamp = time.Now()
	current.Generation++

	snapshot := current.Clone()
	r.mu.Unlock()

	if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixPE, localNodeID), snapshot); err != nil {
		return fmt.Errorf("publish pe intent for %s: %w", localNodeID, err)
	}

	return nil
}

func (r *ServiceRegistry) UpsertPEIntentForNode(
	ctx context.Context,
	nodeID string,
	tenantID string,
	portalID string,
	portal *nodesync.PE_PortalIntent,
	locatorPrefix netip.Prefix,
) error {

	r.mu.Lock()
	current := r.getOrCreateRegistryEntryForPE(nodeID)
	peIntent, ok := current.Intent.(*nodesync.PEIntent)

	if !ok {
		r.mu.Unlock()
		r.logger.ErrorContext(ctx, "failed to cast Intent to PEIntent")
		return fmt.Errorf("failed to cast Intent to PEIntent")
	}

	tenantIntent, err := r.getOrCreateTenantIntentForPE(
		tenantID, locatorPrefix, peIntent,
	)
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("failed to get or create tenant intent for pe: %w", err)
	}

	tenantIntent.PortalIntents[portalID] = *portal

	current.Timestamp = time.Now()
	current.Generation++

	snapshot := current.Clone()

	r.mu.Unlock()

	if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixPE, nodeID), snapshot); err != nil {
		return fmt.Errorf("publish pe intent for %s: %w", nodeID, err)
	}

	return nil
}

func (r *ServiceRegistry) UpsertCPEIntentForSite(ctx context.Context, tenantID string, portalID string, cpe *nodesync.CPEIntent) error {
	r.mu.Lock()
	current, exists := r.registry[portalID]
	if !exists {
		r.registry[portalID] = &nodesync.NodeIntent{
			NodeType:   nodesync.NodeTypeCPE,
			Intent:     cpe,
			Timestamp:  time.Now(),
			Generation: 1,
			Version:    1,
		}
		current.Intent = cpe
		current.Generation = 0
		current.Timestamp = time.Now()
		current.Version = 1
	} else {
		current.Intent = cpe
		current.Generation++
		current.Timestamp = time.Now()
	}

	snapshot := current.Clone()

	r.mu.Unlock()

	if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixCPE, portalID), snapshot); err != nil {
		return fmt.Errorf("failed to publish intent for portalID: %s, err = %w", portalID, err)
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
			if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixCPE, i.PortalID), ni); err != nil {
				return fmt.Errorf("republish intent for %s: %w", i.PortalID, err)
			}
		case *nodesync.PEIntent:
			if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixPE, i.NodeID), ni); err != nil {
				return fmt.Errorf("republish intent for %s: %w", i.NodeID, err)
			}
		}
	}

	return nil
}

func (r *ServiceRegistry) UpsertPeersForPE(
	ctx context.Context,
	nodeID string,
	peers map[string]nodesync.PeerIntent,
) error {
	r.mu.Lock()

	current := r.getOrCreateRegistryEntryForPE(nodeID)
	intent, ok := current.Intent.(*nodesync.PEIntent)
	if !ok {
		return errors.New("failed to cast Intent to PEIntent")
	}

	intent.Peers = peers
	current.Timestamp = time.Now()
	current.Generation++

	snapshot := current.Clone()

	r.mu.Unlock()

	if _, err := r.publisher.Publish(ctx, nodesync.Key(nodesync.PrefixPE, nodeID), snapshot); err != nil {
		return fmt.Errorf("publish pe intent for %s: %w", nodeID, err)
	}

	return nil
}
