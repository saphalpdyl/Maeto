package maetoagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type Agent struct {
	js     jetstream.JetStream
	node   *Node
	logger *slog.Logger
}

func NewAgent(node *Node, js jetstream.JetStream, logger *slog.Logger) *Agent {
	return &Agent{
		js:     js,
		node:   node,
		logger: logger,
	}
}

func (a *Agent) Node() *Node {
	return a.node
}

func (a *Agent) Run(ctx context.Context) {
	a.logger.InfoContext(ctx, "agent starting",
		log.NodeName(a.node.Name),
		log.Locator(a.node.Locator),
		slog.Int("core_links", len(a.node.CoreInterfaces())),
		slog.Bool("access_side", a.node.HasAccessSide()),
	)

	if !a.waitForReady(ctx) {
		return
	}

	a.logger.InfoContext(ctx, "control plane ready",
		log.Domain(log.DomainControlPlane),
		slog.String("intent_key", a.node.IntentKey()),
	)

	if err := a.WatchIntents(ctx, a.js); err != nil {
		a.logger.ErrorContext(ctx, "intent watch failed",
			log.Domain(log.DomainControlPlane),
			slog.String("intent_key", a.node.IntentKey()),
			log.Err(err),
		)
	}

	a.logger.InfoContext(ctx, "agent shutting down")
}

func (a *Agent) waitForReady(ctx context.Context) bool {
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		attempt++

		data, err := a.js.Conn().Request("maeto.control.health.ready", nil, time.Second)
		if err != nil {
			a.logger.WarnContext(ctx, "control plane unreachable",
				log.Domain(log.DomainControlPlane),
				log.Attempt(attempt),
				log.Err(err),
			)
		} else {
			var resp struct {
				Ready string `json:"ready"`
			}

			if err = json.Unmarshal(data.Data, &resp); err != nil {
				a.logger.ErrorContext(ctx, "failed to parse health response",
					log.Domain(log.DomainControlPlane),
					log.Err(err),
				)
			} else if resp.Ready == "true" {
				return true
			}
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
}
