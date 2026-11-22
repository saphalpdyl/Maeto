package controlplane

import (
	"context"
	"log/slog"
	"net/netip"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

const (
	topologyReportStaleAfter    = 30 * time.Second
	topologyReportSweepInterval = 10 * time.Second
	topologyReportRewatchDelay  = 2 * time.Second
	topologySettleDelay         = 1 * time.Second

	trustNodeReadySpan = 20 * time.Second
)

// TopologyReportCollector feeds what nodes report on their topology lane into
// the topology manager. A node that stops reporting loses its End.X SIDs, so
// the PCE never steers onto a SID that may no longer exist.
type TopologyReportCollector struct {
	js       jetstream.JetStream
	logger   *slog.Logger
	topology *ClabTopologyManager

	reportedAt map[NodeID]time.Time

	pceImmediateRecomputeChan chan struct{}

	ready      map[NodeID]bool
	readySince map[NodeID]time.Time
}

func NewTopologyReportCollector(
	js jetstream.JetStream,
	logger *slog.Logger,
	pceImmediateRecomputeChan chan struct{},
	topology *ClabTopologyManager,
) *TopologyReportCollector {
	return &TopologyReportCollector{
		js:                        js,
		logger:                    logger,
		topology:                  topology,
		reportedAt:                make(map[NodeID]time.Time),
		pceImmediateRecomputeChan: pceImmediateRecomputeChan,
		ready:                     map[NodeID]bool{},
		readySince:                make(map[NodeID]time.Time),
	}
}

func (t *TopologyReportCollector) Run(ctx context.Context) {
	feed := make(chan nodesync.TopologyReport, 32)
	go t.watch(ctx, feed)

	sweep := time.NewTicker(topologyReportSweepInterval)
	defer sweep.Stop()

	// Debounce (collect report for a sec before sending it since node-down events send a burst of events)
	var recomputeIn <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case report := <-feed:
			changed := t.apply(ctx, report)
			if changed && recomputeIn == nil {
				recomputeIn = time.After(topologySettleDelay)
			}
		case <-recomputeIn:
			recomputeIn = nil
			t.evaluateNodeDown(ctx)
			select {
			case t.pceImmediateRecomputeChan <- struct{}{}:
			default:
			}
		case <-sweep.C:
			t.evaluateNodeDown(ctx)
			t.clearStale(ctx)
		}
	}
}

func (t *TopologyReportCollector) evaluateNodeDown(ctx context.Context) {
	fresh := make(map[NodeID]bool)
	ready := make(map[NodeID]bool)

	for id, reportedAt := range t.reportedAt {
		if time.Since(reportedAt) >= topologyReportStaleAfter {
			continue
		}

		fresh[id] = true

		since, ok := t.readySince[id]
		ready[id] = t.ready[id] && ok && time.Since(since) >= trustNodeReadySpan
	}

	changed := t.topology.SetDownNodes(fresh, ready)
	for _, c := range changed {
		if t.topology.IsNodeDown(c) {
			t.ready[c] = false
			delete(t.readySince, c)
		}
	}

	if len(changed) > 0 {
		t.logger.WarnContext(ctx, "node down state changed", slog.Any("nodes", changed))
	}
}

func (t *TopologyReportCollector) watch(ctx context.Context, feed chan<- nodesync.TopologyReport) {
	key := nodesync.Key(nodesync.PrefixPE, "*", nodesync.LaneTopology)

	for ctx.Err() == nil {
		err := nodesync.Watch(ctx, t.js, t.logger, nodesync.NodeStateBucket, key, feed)
		if err != nil {
			t.logger.ErrorContext(ctx, "topology report watch failed; rewatching", log.Err(err))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(topologyReportRewatchDelay):
		}
	}
}

func (t *TopologyReportCollector) apply(ctx context.Context, report nodesync.TopologyReport) bool {
	nodeID := NodeID(report.NodeID)

	sidsByIface := make(map[string]netip.Addr, len(report.Adjacencies))
	for _, adjacency := range report.Adjacencies {
		sidsByIface[adjacency.Interface] = adjacency.EndX
	}

	changed := t.topology.SetEndXSIDs(nodeID, sidsByIface)
	if len(changed) > 0 {
		t.logger.InfoContext(ctx, "End.X SIDs changed",
			log.NodeID(string(nodeID)),
			slog.Any("edges", changed))
	}

	linkStateChanged := t.topology.SetLinkStates(nodeID, report.Links)
	if len(linkStateChanged) > 0 {
		t.logger.InfoContext(ctx, "link states linkStateChanged",
			log.NodeID(string(nodeID)),
			slog.Any("edges", linkStateChanged),
			slog.Any("links", report.Links))
	}

	t.reportedAt[nodeID] = report.ReportedAt

	if !report.Ready {
		delete(t.readySince, nodeID)
	} else if _, ok := t.readySince[nodeID]; !ok {
		t.readySince[nodeID] = time.Now()
	}

	readyFlipped := t.ready[nodeID] != report.Ready
	t.ready[nodeID] = report.Ready

	return len(changed) > 0 || len(linkStateChanged) > 0 || readyFlipped
}

func (t *TopologyReportCollector) clearStale(ctx context.Context) {
	for nodeID, reportedAt := range t.reportedAt {
		silentFor := time.Since(reportedAt)
		if silentFor < topologyReportStaleAfter {
			continue
		}

		t.logger.WarnContext(ctx, "topology report is stale; clearing its End.X SIDs",
			log.NodeID(string(nodeID)),
			slog.Duration("silent_for", silentFor))

		t.topology.SetEndXSIDs(nodeID, nil)
		delete(t.reportedAt, nodeID)
	}
}
