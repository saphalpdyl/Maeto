package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/libs/store"
)

// RegistryTx contains a stage-and-commit mechanism for PE/CPE updates. It was built
// to prevent reconciler thrashing due to each minor updates being a generation bump
type RegistryTx struct {
	r *ServiceRegistry
	// Stores the pending mutation functions
	pending map[string][]pendingMutation
}

type pendingMutation struct {
	NodeType nodesync.NodeType
	EntryKey string // key for NodeIntent from r.registry
	Mutate   func(intent nodesync.Intent) error
}

func (r *ServiceRegistry) BeginTx() *RegistryTx {
	return &RegistryTx{
		r:       r,
		pending: make(map[string][]pendingMutation),
	}
}

func (tx *RegistryTx) MutatePE(nodeID NodeID, mutate func(intent *nodesync.PEIntent) error) {
	tx.pending[string(nodeID)] = append(tx.pending[string(nodeID)], pendingMutation{
		NodeType: nodesync.NodeTypePE,
		EntryKey: string(nodeID),
		Mutate: func(i nodesync.Intent) error {
			pe, ok := i.(*nodesync.PEIntent)
			if !ok {
				return fmt.Errorf("intent for pe %s is %T", nodeID, i)
			}

			err := mutate(pe)
			return err
		},
	})
}

func (tx *RegistryTx) Commit(ctx context.Context) error {
	snapshots := make(map[string]*nodesync.NodeIntent)
	collectedErrors := make([]error, 0)

	// Loop through a unit of updates i.e. NodeIntent at a time
	// []mutations for each node is one snapshot publish instead of len(mutations) publish
	tx.r.mu.Lock()
	for entryKey, mutations := range tx.pending {
		intentEntry := tx.r.registry[entryKey]
		if intentEntry == nil {
			// intentEntry is nil, so we get the NodeType from the first pendingMutation
			switch mutations[0].NodeType {
			case nodesync.NodeTypePE:
				intentEntry = tx.r.getOrCreateRegistryEntryForPE(entryKey)
			case nodesync.NodeTypeCPE:
				intentEntry = tx.r.getOrCreateRegistryEntryForCPE(entryKey)
			default:
				tx.r.logger.ErrorContext(ctx, "found unknown intent node type", "entryKey", entryKey, "nodeType", mutations[0].NodeType)
				continue
			}
		}

		for _, m := range mutations {
			err := m.Mutate(intentEntry.Intent)
			collectedErrors = append(collectedErrors, err)
			if err != nil {
				tx.r.logger.ErrorContext(ctx, fmt.Sprintf("failed to mutate intent entry for %s: %v", entryKey, err))
			}
		}

		intentEntry.Timestamp = time.Now()
		intentEntry.Generation++

		var publishTargetKey string

		switch intentEntry.NodeType {
		case nodesync.NodeTypePE:
			publishTargetKey = nodesync.Key(nodesync.PrefixPE, entryKey, nodesync.LaneIntent)
		case nodesync.NodeTypeCPE:
			publishTargetKey = nodesync.Key(nodesync.PrefixCPE, entryKey, nodesync.LaneIntent)
		default:
			err := fmt.Errorf("found unknown intent node type: %v", intentEntry.NodeType)
			collectedErrors = append(collectedErrors, err)
			continue
		}

		snapshots[publishTargetKey] = intentEntry.Clone()
	}
	tx.r.mu.Unlock()

	for publishTargetKey, snapshot := range snapshots {
		err := tx.r.db.Queries.UpsertNodeGeneration(ctx, store.UpsertNodeGenerationParams{
			NodeType:   string(snapshot.NodeType),
			NodeID:     snapshot.ID,
			Version:    int64(snapshot.Version),
			Generation: int64(snapshot.Generation),
		})
		if err != nil {
			tx.r.logger.ErrorContext(ctx, fmt.Sprintf("failed to upsert node generation for snapshot %s: %v", publishTargetKey, err))
			continue
		}

		if _, err := tx.r.publisher.Publish(ctx, publishTargetKey, snapshot); err != nil {
			collectedErrors = append(collectedErrors, err)
			tx.r.logger.ErrorContext(ctx, "failed to publish intent", "publish_target", publishTargetKey, "err", err)
		}
	}

	return errors.Join(collectedErrors...)
}
