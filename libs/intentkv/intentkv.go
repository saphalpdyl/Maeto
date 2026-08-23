// Package intentkv is the jetstream kv transport for node intents, shared by
// the control plane (which publishes) and the agent and portal (which watch).
package intentkv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/avast/retry-go"
	"github.com/nats-io/nats.go/jetstream"
)

const Bucket = "maeto-intents"

// key prefixes, so a pe and a cpe cannot collide on the same id
const (
	PrefixPE  = "pop"
	PrefixCPE = "cpe"
)

func Key(prefix, id string) string {
	return fmt.Sprintf("%s.%s", prefix, id)
}

// Publisher owns the bucket. Ensure is safe to call again after a nats
// reconnect, where the server may have lost the bucket entirely.
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
		Description: "desired dataplane state per node",
		History:     1,
		Storage:     jetstream.FileStorage,
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

func (p *Publisher) Publish(ctx context.Context, key string, intent any) (uint64, error) {
	data, err := json.Marshal(intent)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal intent for %s: %w", key, err)
	}

	rev, err := p.bucket().Put(ctx, key, data)
	if err != nil {
		return 0, fmt.Errorf("failed to publish intent for %s: %w", key, err)
	}

	return rev, nil
}

func (p *Publisher) Current(ctx context.Context, key string, out any) (uint64, error) {
	entry, err := p.bucket().Get(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("failed to read intent for %s: %w", key, err)
	}

	if err = json.Unmarshal(entry.Value(), out); err != nil {
		return 0, fmt.Errorf("failed to parse intent for %s: %w", key, err)
	}

	return entry.Revision(), nil
}

const (
	openAttempts = 200
	openDelay    = 2 * time.Second
)

// Open waits for the bucket to exist. The control plane owns it, so a node that
// starts first -- or outlives a nats restart -- has to wait rather than give up.
func Open(ctx context.Context, js jetstream.JetStream, logger *slog.Logger) (jetstream.KeyValue, error) {
	var kv jetstream.KeyValue

	err := retry.Do(func() error {
		var err error
		kv, err = js.KeyValue(ctx, Bucket)

		return err
	},
		retry.Context(ctx),
		retry.Attempts(openAttempts),
		retry.Delay(openDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(n uint, err error) {
			logger.WarnContext(ctx, "intent bucket unavailable",
				slog.String("bucket", Bucket),
				slog.Int("attempt", int(n)+1),
				slog.String("error", err.Error()),
			)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open kv bucket %s: %w", Bucket, err)
	}

	return kv, nil
}

// Watch delivers every intent written to key onto feed until ctx is done. T is
// the payload the caller expects: the agent and the portal decode the same
// bucket into their own shapes.
func Watch[T any](ctx context.Context, js jetstream.JetStream, logger *slog.Logger, key string, feed chan<- *T) error {
	kv, err := Open(ctx, js, logger)
	if err != nil {
		return err
	}

	watcher, err := kv.Watch(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to watch %s: %w", key, err)
	}
	defer func() {
		if err := watcher.Stop(); err != nil {
			logger.ErrorContext(ctx, "failed to stop intent watcher", slog.String("error", err.Error()))
		}
	}()

	logger.InfoContext(ctx, "watching intents",
		slog.String("bucket", Bucket),
		slog.String("key", key),
	)

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return fmt.Errorf("intent watcher closed")
			}

			// nil marks the end of the initial replay
			if entry == nil {
				logger.InfoContext(ctx, "initial intent replay complete")

				continue
			}

			if entry.Operation() != jetstream.KeyValuePut {
				continue
			}

			var intent T
			if err := json.Unmarshal(entry.Value(), &intent); err != nil {
				logger.ErrorContext(ctx, "failed to parse intent",
					slog.Uint64("revision", entry.Revision()),
					slog.String("error", err.Error()),
				)

				continue
			}

			logger.InfoContext(ctx, "intent received",
				slog.Uint64("revision", entry.Revision()),
				slog.Any("intent", intent),
			)

			select {
			case feed <- &intent:
			case <-ctx.Done():
				return nil
			}
		}
	}
}
