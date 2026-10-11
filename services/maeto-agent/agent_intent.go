package maetoagent

import (
	"context"
	"log/slog"
	"time"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

func (a *Agent) fanOutToFeeds(ctx context.Context, item *nodesync.NodeIntent) {
	select {
	case a.dataplaneIntentFeed <- item.Clone():
	default:
		a.logger.WarnContext(ctx, "dropped dataplane intent, feed seems full", slog.Any("item", item.Clone()))
	}

	select {
	case a.probeIntentFeed <- item.Clone():
	default:
		a.logger.WarnContext(ctx, "dropped probe intent, feed seems full", slog.Any("item", item.Clone()))
	}
}

func (a *Agent) setupIntentWatch(ctx context.Context) {
	aggregateFeed := make(chan *nodesync.NodeIntent, 32)

	resync := time.NewTicker(20 * time.Second)
	defer resync.Stop()

	var last *nodesync.NodeIntent

	// Fan-out intent to multiple receivers
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case item, ok := <-aggregateFeed:
				if !ok {
					return
				}
				a.fanOutToFeeds(ctx, item)
				last = item.Clone()
			case <-resync.C:
				if last != nil {
					a.fanOutToFeeds(ctx, last)
				}
			}
		}
	}(ctx)

	err := nodesync.Watch(
		ctx,
		a.js,
		a.logger.With(log.Domain(log.DomainControlPlane)),
		nodesync.IntentBucket,
		nodesync.Key(nodesync.PrefixPE, a.node.ID, nodesync.LaneIntent),
		aggregateFeed,
	)

	if err != nil {
		a.logger.ErrorContext(ctx, "intent watch failed",
			log.Domain(log.DomainControlPlane),
			slog.String("intent_key", a.node.IntentKey()),
			log.Err(err),
		)
	}
}
