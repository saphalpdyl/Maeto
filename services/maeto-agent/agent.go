package maetoagent

import (
	"context"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/strongswan/govici/vici"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/intent"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/libs/statekv"
	"github.com/saphalpdyl/maeto/libs/transport"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type Agent struct {
	js         jetstream.JetStream
	node       *Node
	logger     *slog.Logger
	reconciler *dataplane.Reconciler
	dp         dataplane.Dataplane // owned primarily by the Reconciler

	// Intents pushed to by the intentkv watcher and read by the Reconciler
	intentFeed chan *intent.NodeIntent

	probeSupervisor *probe.Supervisor
}

func NewAgent(node *Node, js jetstream.JetStream, logger *slog.Logger, dp dataplane.Dataplane) *Agent {
	intentFeed := make(chan *intent.NodeIntent, 32)
	reconciler := dataplane.NewReconciler(dp, intent.NodeTypePE, logger.With(log.Domain(log.DomainReconciler)), intentFeed)

	return &Agent{
		js:              js,
		node:            node,
		logger:          logger,
		reconciler:      reconciler,
		intentFeed:      intentFeed,
		dp:              dp,
		probeSupervisor: nil,
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

	if publisher, err := statekv.NewPublisher(ctx, a.js); err != nil {
		a.logger.ErrorContext(ctx, "failed to open state bucket",
			log.Domain(log.DomainControlPlane),
			log.Err(err),
		)
	} else {
		a.reconciler.SetStateReporter(&stateReporter{
			publisher: publisher,
			key:       statekv.Key(statekv.PrefixPE, a.node.ID),
			nodeID:    a.node.ID,
		})
	}

	go a.reconciler.Start(ctx) // nolint:errcheck

	go func() {
		err := transport.Watch(
			ctx,
			a.js,
			a.logger.With(log.Domain(log.DomainControlPlane)),
			transport.Key(transport.PrefixPE, a.node.ID),
			a.intentFeed,
		)

		if err != nil {
			a.logger.ErrorContext(ctx, "intent watch failed",
				log.Domain(log.DomainControlPlane),
				slog.String("intent_key", a.node.IntentKey()),
				log.Err(err),
			)
		}
	}()

	s, err := vici.NewSession()
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to open vici session", log.Err(err))
		return
	}
	defer s.Close() // nolint:errcheck

	eventChan, err := a.watchEvents(ctx, s)
	if err != nil {
		return
	}

	go a.watchTunnelUpdates(ctx, eventChan)

	if a.node.Access != nil {
		// Loads private key from /etc/swanctl/private/key.pem and sends to charon via vici
		if err := a.loadCredentials(ctx, s); err != nil {
			return
		}

		// Loads connection from /etc/swanctl/x509/cert.pem and /etc/swanctl/x509ca/ca-cert.pem and sends to charon via vici
		if err := a.loadConnection(ctx, s); err != nil {
			return
		}
	}

	<-ctx.Done()
}
