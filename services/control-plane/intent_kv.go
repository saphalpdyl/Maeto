package controlplane

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

const IntentBucket = "maeto-intents"

type IntentPublisher struct {
	kv jetstream.KeyValue
}

func NewIntentPublisher(ctx context.Context, js jetstream.JetStream) (*IntentPublisher, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      IntentBucket,
		Description: "desired dataplane state per pop",
		History:     1,
		Storage:     jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create kv bucket %s: %w", IntentBucket, err)
	}

	return &IntentPublisher{kv: kv}, nil
}

func IntentKey(node NodeID) string {
	return fmt.Sprintf("pop.%s", node)
}

func (p *IntentPublisher) Publish(ctx context.Context, node NodeID, intent any) (uint64, error) {
	data, err := json.Marshal(intent)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal intent for %s: %w", node, err)
	}

	rev, err := p.kv.Put(ctx, IntentKey(node), data)
	if err != nil {
		return 0, fmt.Errorf("failed to publish intent for %s: %w", node, err)
	}

	return rev, nil
}

func (p *IntentPublisher) Current(ctx context.Context, node NodeID, out any) (uint64, error) {
	entry, err := p.kv.Get(ctx, IntentKey(node))
	if err != nil {
		return 0, fmt.Errorf("failed to read intent for %s: %w", node, err)
	}

	if err = json.Unmarshal(entry.Value(), out); err != nil {
		return 0, fmt.Errorf("failed to parse intent for %s: %w", node, err)
	}

	return entry.Revision(), nil
}
