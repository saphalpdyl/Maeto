package nodesync

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const TTL = 90 * time.Second

// key prefixes, so a pe and a cpe cannot collide on the same id
const (
	PrefixPE  = "pop"
	PrefixCPE = "cpe"
)

const KeyControlSnapshot = "snapshot"

type BucketConfig struct {
	Name        string
	Description string
	TTL         time.Duration
}

var (
	IntentBucket = BucketConfig{
		Name:        "maeto-intents",
		Description: "desired dataplane state per node",
	}

	NodeStateBucket = BucketConfig{
		Name:        "maeto-state",
		Description: "observed dataplane state per node",
		TTL:         TTL,
	}

	ControlStateBucket = BucketConfig{
		Name:        "maeto-control",
		Description: "control plane topology, inventory and registry snapshot",
		TTL:         TTL,
	}
)

func Key(prefix, id string) string {
	return fmt.Sprintf("%s.%s", prefix, id)
}

// Publisher owns its bucket. Ensure is safe to call again after a nats
// reconnect, where the server may have lost the bucket entirely.
type Publisher struct {
	js  jetstream.JetStream
	cfg BucketConfig

	mu sync.RWMutex
	kv jetstream.KeyValue
}

func NewPublisher(ctx context.Context, js jetstream.JetStream, cfg BucketConfig) (*Publisher, error) {
	p := &Publisher{js: js, cfg: cfg}
	if err := p.Ensure(ctx); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *Publisher) Ensure(ctx context.Context) error {
	kv, err := p.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      p.cfg.Name,
		Description: p.cfg.Description,
		History:     1,
		Storage:     jetstream.FileStorage,
		TTL:         p.cfg.TTL,
	})
	if err != nil {
		return fmt.Errorf("failed to create kv bucket %s: %w", p.cfg.Name, err)
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

func (p *Publisher) Publish(ctx context.Context, key string, value any) (uint64, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal %s for %s: %w", p.cfg.Name, key, err)
	}

	rev, err := p.bucket().Put(ctx, key, data)
	if err != nil {
		return 0, fmt.Errorf("failed to publish %s for %s: %w", p.cfg.Name, key, err)
	}

	return rev, nil
}

func (p *Publisher) Current(ctx context.Context, key string, out any) (uint64, error) {
	entry, err := p.bucket().Get(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("failed to read %s for %s: %w", p.cfg.Name, key, err)
	}

	if err = json.Unmarshal(entry.Value(), out); err != nil {
		return 0, fmt.Errorf("failed to parse %s for %s: %w", p.cfg.Name, key, err)
	}

	return entry.Revision(), nil
}
