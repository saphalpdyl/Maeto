package maetoportal

import (
	"context"
	"log/slog"
	"time"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
)

func (p *Portal) fanOutToFeeds(ctx context.Context, item *nodesync.NodeIntent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, feed := range p.intentFeeds {
		select {
		case feed <- item.Clone():
		default:
			p.logger.WarnContext(ctx, "dropped intent, feed seems full", slog.Any("item", item.Clone()))
		}
	}
}

func (p *Portal) createIntentConsumer() chan *nodesync.NodeIntent {
	p.mu.Lock()
	defer p.mu.Unlock()

	intentFeed := make(chan *nodesync.NodeIntent, 32)
	p.intentFeeds = append(p.intentFeeds, intentFeed)
	return intentFeed
}

func (p *Portal) setupIntentWatch(ctx context.Context) {
	aggregateFeed := make(chan *nodesync.NodeIntent, 32)

	resync := time.NewTicker(20 * time.Second)
	defer resync.Stop()

	var last *nodesync.NodeIntent

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case item, ok := <-aggregateFeed:
				if !ok {
					return
				}
				p.fanOutToFeeds(ctx, item)
				last = item.Clone()
			case <-resync.C:
				if last != nil {
					p.fanOutToFeeds(ctx, last)
				}
			}
		}
	}(ctx)

	err := nodesync.Watch(
		ctx,
		p.js,
		p.logger.With(log.Domain(log.DomainControlPlane)),
		nodesync.IntentBucket,
		nodesync.Key(nodesync.PrefixCPE, p.config.PortalID, nodesync.LaneIntent),
		aggregateFeed,
	)

	if err != nil {
		p.logger.ErrorContext(ctx, "intent watch failed",
			log.Domain(log.DomainControlPlane),
			log.Err(err),
		)
	}
}
