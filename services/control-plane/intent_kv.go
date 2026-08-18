package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)

const IntentBucket = "maeto-intents"

type IntentPublisher struct {
	js jetstream.JetStream

	mu sync.RWMutex
	kv jetstream.KeyValue
}

func NewIntentPublisher(ctx context.Context, js jetstream.JetStream) (*IntentPublisher, error) {
	p := &IntentPublisher{js: js}
	if err := p.Ensure(ctx); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *IntentPublisher) Ensure(ctx context.Context) error {
	kv, err := p.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      IntentBucket,
		Description: "desired dataplane state per pop",
		History:     1,
		Storage:     jetstream.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("failed to create kv bucket %s: %w", IntentBucket, err)
	}

	p.mu.Lock()
	p.kv = kv
	p.mu.Unlock()

	return nil
}

func (p *IntentPublisher) bucket() jetstream.KeyValue {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.kv
}

func IntentKey(node NodeID) string {
	return fmt.Sprintf("pop.%s", node)
}

func (p *IntentPublisher) Publish(ctx context.Context, node NodeID, intent NodeIntent) (uint64, error) {
	data, err := json.Marshal(intent)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal intent for %s: %w", node, err)
	}

	rev, err := p.bucket().Put(ctx, IntentKey(node), data)
	if err != nil {
		return 0, fmt.Errorf("failed to publish intent for %s: %w", node, err)
	}

	return rev, nil
}

func (p *IntentPublisher) Current(ctx context.Context, node NodeID, out *NodeIntent) (uint64, error) {
	entry, err := p.bucket().Get(ctx, IntentKey(node))
	if err != nil {
		return 0, fmt.Errorf("failed to read intent for %s: %w", node, err)
	}

	if err = json.Unmarshal(entry.Value(), out); err != nil {
		return 0, fmt.Errorf("failed to parse intent for %s: %w", node, err)
	}

	return entry.Revision(), nil
}
