package maetoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

const IntentBucket = "maeto-intents"

type NodeIntent struct {
	Node     string
	Gen      int32
	Snapshot bool
	Hash     string
	Ops      []json.RawMessage
}

func (a *Agent) WatchIntents(ctx context.Context, js jetstream.JetStream) error {
	kv, err := js.KeyValue(ctx, IntentBucket)
	if err != nil {
		return fmt.Errorf("failed to open kv bucket %s: %w", IntentBucket, err)
	}

	key := a.node.IntentKey()

	watcher, err := kv.Watch(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to watch %s: %w", key, err)
	}
	defer func() {
		if err := watcher.Stop(); err != nil {
			a.logger.ErrorContext(ctx, "failed to stop intent watcher", log.Err(err))
		}
	}()

	a.logger.InfoContext(ctx, "watching intents",
		log.Domain(log.DomainControlPlane),
		slog.String("bucket", IntentBucket),
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

			if entry == nil {
				a.logger.InfoContext(ctx, "initial intent replay complete",
					log.Domain(log.DomainControlPlane),
				)

				continue
			}

			if entry.Operation() != jetstream.KeyValuePut {
				continue
			}

			var intent NodeIntent
			if err := json.Unmarshal(entry.Value(), &intent); err != nil {
				a.logger.ErrorContext(ctx, "failed to parse intent",
					log.Domain(log.DomainControlPlane),
					slog.Uint64("revision", entry.Revision()),
					log.Err(err),
				)

				continue
			}

			a.logger.InfoContext(ctx, "intent received",
				log.Domain(log.DomainControlPlane),
				slog.Uint64("revision", entry.Revision()),
				slog.Int("gen", int(intent.Gen)),
				slog.Bool("snapshot", intent.Snapshot),
				slog.Int("ops", len(intent.Ops)),
			)
		}
	}
}
