package nodesync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/avast/retry-go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	openAttempts = 200
	openDelay    = 2 * time.Second
)

// Open waits for the bucket to exist. The control plane owns it, so a node that
// starts first -- or outlives a nats restart -- has to wait rather than give up.
func Open(ctx context.Context, js jetstream.JetStream, logger *slog.Logger, cfg BucketConfig) (jetstream.KeyValue, error) {
	var kv jetstream.KeyValue

	err := retry.Do(func() error {
		var err error
		kv, err = js.KeyValue(ctx, cfg.Name)

		return err
	},
		retry.Context(ctx),
		retry.Attempts(openAttempts),
		retry.Delay(openDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(n uint, err error) {
			logger.WarnContext(ctx, "bucket unavailable",
				slog.String("bucket", cfg.Name),
				slog.Int("attempt", int(n)+1),
				slog.String("error", err.Error()),
			)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open kv bucket %s: %w", cfg.Name, err)
	}

	return kv, nil
}

// Watch delivers every intent written to key onto feed until ctx is done. T is
// the payload the caller expects: the agent and the portal decode the same
// bucket into their own shapes.
func Watch[T any](ctx context.Context, js jetstream.JetStream, logger *slog.Logger, cfg BucketConfig, key string, feed chan<- *T) error {
	kv, err := Open(ctx, js, logger, cfg)
	if err != nil {
		return err
	}

	watcher, err := kv.Watch(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to watch %s: %w", key, err)
	}
	defer func() {
		if err := watcher.Stop(); err != nil {
			logger.ErrorContext(ctx, "failed to stop watcher", slog.String("bucket", cfg.Name), slog.String("error", err.Error()))
		}
	}()

	logger.InfoContext(ctx, "watching bucket",
		slog.String("bucket", cfg.Name),
		slog.String("key", key),
	)

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return fmt.Errorf("%s watcher closed", cfg.Name)
			}

			// nil marks the end of the initial replay
			if entry == nil {
				logger.InfoContext(ctx, "initial replay complete", slog.String("bucket", cfg.Name))

				continue
			}

			if entry.Operation() != jetstream.KeyValuePut {
				continue
			}

			var value T
			if err := json.Unmarshal(entry.Value(), &value); err != nil {
				logger.ErrorContext(ctx, "failed to parse entry",
					slog.String("bucket", cfg.Name),
					slog.Uint64("revision", entry.Revision()),
					slog.String("error", err.Error()),
				)

				continue
			}

			logger.InfoContext(ctx, "entry received",
				slog.String("bucket", cfg.Name),
				slog.Uint64("revision", entry.Revision()),
				slog.Any("value", value),
			)

			select {
			case feed <- &value:
			case <-ctx.Done():
				return nil
			}
		}
	}
}
