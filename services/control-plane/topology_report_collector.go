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
)

// TopologyReportCollector feeds what nodes report on their topology lane into
// the topology manager. A node that stops reporting loses its End.X SIDs, so
// the PCE never steers onto a SID that may no longer exist.
type TopologyReportCollector struct {
	js       jetstream.JetStream
	logger   *slog.Logger
	topology *ClabTopologyManager

	reportedAt map[NodeID]time.Time
}

func NewTopologyReportCollector(js jetstream.JetStream, logger *slog.Logger, topology *ClabTopologyManager) *TopologyReportCollector {
	return &TopologyReportCollector{
		js:         js,
		logger:     logger,
		topology:   topology,
		reportedAt: make(map[NodeID]time.Time),
	}
}

func (t *TopologyReportCollector) Run(ctx context.Context) {
	feed := make(chan nodesync.TopologyReport, 32)
	go t.watch(ctx, feed)

	sweep := time.NewTicker(topologyReportSweepInterval)
	defer sweep.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case report := <-feed:
			t.apply(ctx, report)
		case <-sweep.C:
			t.clearStale(ctx)
		}
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

func (t *TopologyReportCollector) apply(ctx context.Context, report nodesync.TopologyReport) {
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

	t.reportedAt[nodeID] = report.ReportedAt
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
