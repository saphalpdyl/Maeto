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
	Bucket        = "maeto-state"
	ControlBucket = "maeto-control"
	TTL           = 90 * time.Second
)

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
	NodeState = BucketConfig{
		Name:        Bucket,
		Description: "observed dataplane state per node",
		TTL:         TTL,
	}

	ControlState = BucketConfig{
		Name:        ControlBucket,
		Description: "control plane topology, inventory and registry snapshot",
		TTL:         TTL,
	}
)

func Key(prefix, id string) string {
	return fmt.Sprintf("%s.%s", prefix, id)
}

type Publisher struct {
	js  jetstream.JetStream
	cfg BucketConfig

	mu sync.RWMutex
	kv jetstream.KeyValue
}

func NewPublisher(ctx context.Context, js jetstream.JetStream) (*Publisher, error) {
	return NewPublisherFor(ctx, js, NodeState)
}

func NewPublisherFor(ctx context.Context, js jetstream.JetStream, cfg BucketConfig) (*Publisher, error) {
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
