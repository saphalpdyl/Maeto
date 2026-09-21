package maetoagent

import (
	"context"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/strongswan/govici/vici"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type Agent struct {
	js         jetstream.JetStream
	node       *Node
	logger     *slog.Logger
	reconciler *dataplane.Reconciler
	dp         dataplane.Dataplane // owned primarily by the Reconciler

	// Carries the same intent feed. A goroutine broadcasts
	// received to both these channel
	dataplaneIntentFeed chan *nodesync.NodeIntent
	probeIntentFeed     chan *nodesync.NodeIntent

	probeSupervisor *probe.Supervisor
}

func NewAgent(node *Node, js jetstream.JetStream, logger *slog.Logger, dp dataplane.Dataplane) *Agent {
	dataplaneIntentFeed := make(chan *nodesync.NodeIntent, 32)
	probeIntentFeed := make(chan *nodesync.NodeIntent, 32)

	reconciler := dataplane.NewReconciler(dp, nodesync.NodeTypePE, logger.With(log.Domain(log.DomainReconciler)), dataplaneIntentFeed)

	toLogsDispatcher := probe.NewToLogsDispatcher(logger)
	probeSupervisor := probe.NewSupervisor(probe.SupervisorConfig{
		Dispatcher:  toLogsDispatcher,
		StopTimeout: 0,
		Runner:      probe.NewDefaultRunner(toLogsDispatcher, logger),
	}, logger.With(log.Domain(log.DomainIntentSupervisor)), probeIntentFeed)

	return &Agent{
		js:                  js,
		node:                node,
		logger:              logger,
		reconciler:          reconciler,
		dataplaneIntentFeed: dataplaneIntentFeed,
		probeIntentFeed:     probeIntentFeed,
		dp:                  dp,
		probeSupervisor:     probeSupervisor,
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

	if publisher, err := nodesync.NewPublisher(ctx, a.js, nodesync.NodeStateBucket); err != nil {
		a.logger.ErrorContext(ctx, "failed to open state bucket",
			log.Domain(log.DomainControlPlane),
			log.Err(err),
		)
	} else {
		a.reconciler.SetStateReporter(&stateReporter{
			publisher: publisher,
			key:       nodesync.Key(nodesync.PrefixPE, a.node.ID),
			nodeID:    a.node.ID,
		})
	}

	go a.reconciler.Start(ctx)      // nolint:errcheck
	go a.probeSupervisor.Start(ctx) // nolint:errcheck

	go a.setupIntentWatch(ctx)

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
