package maetoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/failover"
	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type Agent struct {
	mu sync.Mutex

	js         jetstream.JetStream
	node       *Node
	logger     *slog.Logger
	reconciler *dataplane.Reconciler
	dp         dataplane.Dataplane // owned primarily by the Reconciler

	intentFeeds []chan *nodesync.NodeIntent

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

	agent := &Agent{
		js:          js,
		node:        node,
		logger:      logger,
		dp:          dp,
		swanClient:  swanClient,
		intentFeeds: make([]chan *nodesync.NodeIntent, 0),
	}

	dataplaneIntentFeed := agent.createIntentConsumer()
	probeIntentFeed := agent.createIntentConsumer()

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

	agent.reconciler = reconciler
	agent.probeSupervisor = probeSupervisor

	return agent
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

		health := failover.NewHealthMonitor(a.logger.With(log.Domain(log.DomainControlPlane)))
		topologyReporterLinkUpdatesChan := health.CreateConsumer()

		topology := &topologyReporter{
			publisher:   publisher,
			key:         nodesync.Key(nodesync.PrefixPE, a.node.ID, nodesync.LaneTopology),
			nodeID:      a.node.ID,
			dp:          a.dp,
			logger:      a.logger.With(log.Domain(log.DomainControlPlane)),
			linkUpdates: topologyReporterLinkUpdatesChan,
			reconciler:  a.reconciler,
		}
		go topology.Run(ctx)

		failoverLatch := failover.NewFailoverLatch(
			health.CreateConsumer(),
			a.logger.With(log.Domain(log.DomainFailover)),
			func(egress string) error {
				a.logger.ErrorContext(ctx, "EGRESS FAILURE: TRYING TO LOCALLY REPAIR")

				if err := a.dp.ActivateRepair(egress); err != nil {
					a.logger.ErrorContext(ctx, "failed to activate repair", log.Err(err))
					return err
				}

				a.logger.ErrorContext(ctx, "EGRESS FAILURE: REPAIRED SUCCESSFULLY")
				return nil
			},
			func(egress string) error {
				if err := a.dp.ResetRepair(egress, false); err != nil {
					return err
				}

				a.logger.InfoContext(ctx, "failover reset", slog.String("egress", egress))
				return nil
			},
		)
		go failoverLatch.Start(ctx)

		go func(fl *failover.FailoverLatch) {
			failoverIntentFeed := a.createIntentConsumer()
			for {
				select {
				case <-ctx.Done():
					return
				case intent := <-failoverIntentFeed:
					peIntent, ok := intent.Intent.(*nodesync.PEIntent)
					if !ok || peIntent == nil {
						continue
					}

					grouping := make(map[string][]string)

					for _, p := range peIntent.Peers {
						groups, exists := grouping[p.PeerID]
						if !exists {
							grouping[p.PeerID] = make([]string, 0)
						}

						grouping[p.PeerID] = append(groups, p.LocalInterface)
					}

					fl.SetGrouping(grouping)
					fl.MarkReady()
					fl.Recover(ctx, peIntent.DownNodes)
				}
			}
		}(failoverLatch)

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

	// A p router terminates no ipsec: no tunnels to watch, report or resync
	if a.node.HasAccessSide() {
		eventChan, err := a.swanClient.WatchIKEUpdownEvents(ctx)
		if err != nil {
			a.logger.ErrorContext(ctx, "failed to watch events", log.Err(err))
			return
		}

		go a.watchTunnelUpdates(ctx, eventChan)
		go a.setupResyncWatch(ctx)

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
