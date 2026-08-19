package maetoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/avast/retry-go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

const IntentBucket = "maeto-intents"

type NodeIntent struct {
	NodeID        string          `json:"node_id"`
	TenantIntents map[int]*Intent `json:"tenant_intents"`
}

type Site struct {
	TenantID   int
	CPE        string
	PortalID   string
	Node       string
	Prefix     netip.Prefix
	Attach     string
	AttachNode string
	IfID       uint32
	Identity   string
}

type Intent struct {
	Gen   uint64  `json:"gen"`
	Sites []*Site `json:"sites"`
}

func (a *Agent) WatchIntents(ctx context.Context, js jetstream.JetStream) error {
	var kv jetstream.KeyValue
	err := retry.Do(func() error {
		var err error
		kv, err = js.KeyValue(ctx, IntentBucket)
		if err != nil {
			return fmt.Errorf("failed to open kv bucket %s: %w", IntentBucket, err)
		}

		return nil
	},
		retry.Context(ctx),
		retry.Attempts(200),
		retry.Delay(2*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(n uint, err error) {
			a.logger.WarnContext(ctx, "intent bucket unavailable",
				log.Domain(log.DomainControlPlane),
				slog.String("bucket", IntentBucket),
				log.Attempt(int(n)+1),
				log.Err(err),
			)
		}),
	)
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
				slog.Any("intent", intent),
			)

			a.intentFeed <- &intent
		}
	}
}
