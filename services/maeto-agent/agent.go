package maetoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/libs/swan"
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

	swanClient *swan.Client
}

func NewAgent(
	node *Node,
	js jetstream.JetStream,
	logger *slog.Logger,
	dp dataplane.Dataplane,
	swanClient *swan.Client,
) *Agent {
	dataplaneIntentFeed := make(chan *nodesync.NodeIntent, 32)
	probeIntentFeed := make(chan *nodesync.NodeIntent, 32)

	reconciler := dataplane.NewReconciler(dp, nodesync.NodeTypePE, logger.With(log.Domain(log.DomainReconciler)), dataplaneIntentFeed)

	dispatcherConfig := probe.NATSDispatcherConfig{
		SubjectPrefix:  fmt.Sprintf("%s.%s", probe.ProbeResultSubjectPrefix, node.ID),
		PublishTimeout: 5 * time.Second,
	}
	dispatcher := probe.NewNATSDispatcher(js, dispatcherConfig)
	probeSupervisor := probe.NewSupervisor(probe.SupervisorConfig{
		Dispatcher:  dispatcher,
		StopTimeout: 0,
		Runner:      probe.NewDefaultRunner(dispatcher, logger),
	}, logger.With(log.Domain(log.DomainIntentSupervisor)), dp, probeIntentFeed)

	return &Agent{
		js:                  js,
		node:                node,
		logger:              logger,
		reconciler:          reconciler,
		dataplaneIntentFeed: dataplaneIntentFeed,
		probeIntentFeed:     probeIntentFeed,
		dp:                  dp,
		probeSupervisor:     probeSupervisor,
		swanClient:          swanClient,
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
			key:       nodesync.Key(nodesync.PrefixPE, a.node.ID, nodesync.LaneDataplane),
			nodeID:    a.node.ID,
		})

		linkUpdates := make(chan map[string]nodesync.LinkState)

		topology := &topologyReporter{
			publisher:   publisher,
			key:         nodesync.Key(nodesync.PrefixPE, a.node.ID, nodesync.LaneTopology),
			nodeID:      a.node.ID,
			dp:          a.dp,
			logger:      a.logger.With(log.Domain(log.DomainControlPlane)),
			linkUpdates: linkUpdates,
		}
		go topology.Run(ctx)

		health := NewHealthMonitor(linkUpdates, a.logger.With(log.Domain(log.DomainControlPlane)))
		go func() {
			if err := health.Start(ctx); err != nil && ctx.Err() == nil {
				a.logger.ErrorContext(ctx, "health monitor stopped", log.Err(err))
			}
		}()
	}

	go func() {
		err := a.reconciler.Start(ctx)
		if err != nil {
			a.logger.ErrorContext(ctx, "failed to start reconciler", log.Err(err))
		}
	}() // nolint:errcheck
	go a.probeSupervisor.Start(ctx) // nolint:errcheck

	go a.setupIntentWatch(ctx)

	eventChan, err := a.swanClient.WatchIKEUpdownEvents(ctx)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to watch events", log.Err(err))
		return
	}

	go a.watchTunnelUpdates(ctx, eventChan)
	go a.setupResyncWatch(ctx)

	if a.node.Access != nil {
		// Loads private key from /etc/swanctl/private/key.pem and sends to charon via vici
		if err := a.loadCredentials(ctx); err != nil {
			return
		}

		// Loads connection from /etc/swanctl/x509/cert.pem and /etc/swanctl/x509ca/ca-cert.pem and sends to charon via vici
		if err := a.loadConnection(ctx); err != nil {
			return
		}
	}

	<-ctx.Done()
}

// Waits for the control plane to reach healthy status
func (a *Agent) waitForReady(ctx context.Context) bool {
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		attempt++

		data, err := a.js.Conn().Request(controlapi.SubjectHealthReady, nil, time.Second)
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
		case <-time.After(2000 * time.Millisecond):
		}
	}
}

func (a *Agent) setupResyncWatch(ctx context.Context) {
	_, _ = a.js.Conn().Subscribe(controlapi.SubjectControllerPETunnelResync, func(msg *nats.Msg) {
		sas, err := a.swanClient.GetSAs(ctx)
		if err != nil {
			a.logger.ErrorContext(ctx, "failed to get SAS info", log.Err(err))
		}

		// Loop through the list of IKE_SA
		for _, sa := range sas {
			err := a.sendPETunnelUpdate(ctx, sa)
			if err != nil {
				a.logger.ErrorContext(ctx, "failed to send CPETunnelUpdate", log.Err(err))
				continue
			}
		}

		if err := msg.Ack(); err != nil {
			a.logger.ErrorContext(ctx, "failed to ack message", log.Err(err))
		}
	})

	<-ctx.Done()
}
