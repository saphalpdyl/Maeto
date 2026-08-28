package statekv

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	Bucket = "maeto-state"
	TTL    = 90 * time.Second
)

const (
	PrefixPE  = "pop"
	PrefixCPE = "cpe"
)

func Key(prefix, id string) string {
	return fmt.Sprintf("%s.%s", prefix, id)
}

type Publisher struct {
	js jetstream.JetStream

	mu sync.RWMutex
	kv jetstream.KeyValue
}

func NewPublisher(ctx context.Context, js jetstream.JetStream) (*Publisher, error) {
	p := &Publisher{js: js}
	if err := p.Ensure(ctx); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *Publisher) Ensure(ctx context.Context) error {
	kv, err := p.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      Bucket,
		Description: "observed dataplane state per node",
		History:     1,
		Storage:     jetstream.FileStorage,
		TTL:         TTL,
	})
	if err != nil {
		return fmt.Errorf("failed to create kv bucket %s: %w", Bucket, err)
	}

	p.mu.Lock()
	p.kv = kv
	p.mu.Unlock()

	return nil
}

func (p *Publisher) bucket() jetstream.KeyValue {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.kv
}

func (p *Publisher) Publish(ctx context.Context, key string, state any) (uint64, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal state for %s: %w", key, err)
	}

	rev, err := p.bucket().Put(ctx, key, data)
	if err != nil {
		return 0, fmt.Errorf("failed to publish state for %s: %w", key, err)
	}

	return rev, nil
}
