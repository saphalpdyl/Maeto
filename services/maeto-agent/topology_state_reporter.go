package maetoagent

// Reports local SID allocations and links states to the PCE

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"golang.org/x/sys/unix"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

const (
	topologyReportInterval      = 10 * time.Second
	topologyReportRetryInterval = time.Second
)

type topologyReporter struct {
	publisher *nodesync.Publisher
	key       string
	nodeID    string
	dp        dataplane.Dataplane
	logger    *slog.Logger

	linkUpdates <-chan map[string]nodesync.LinkState
	links       map[string]nodesync.LinkState
}

func (t *topologyReporter) Run(ctx context.Context) {
	ticker := time.NewTicker(topologyReportInterval)
	defer ticker.Stop()

	for {
		// Retries rebuild the report, so the latest state is what gets stored.
		var retry <-chan time.Time
		if !t.reportOnce(ctx) {
			retry = time.After(topologyReportRetryInterval)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-retry:
		case t.links = <-t.linkUpdates:
		}
	}
}

// reportOnce reports whether the report was stored (acked by JetStream).
func (t *topologyReporter) reportOnce(ctx context.Context) bool {
	report, err := t.collect()
	if err != nil {
		t.logger.ErrorContext(ctx, "failed to collect topology report", log.Err(err))
		return false
	}

	if _, err := t.publisher.Publish(ctx, t.key, report); err != nil {
		t.logger.ErrorContext(ctx, "failed to publish topology report", log.Err(err))
		return false
	}

	return true
}

// End.X SIDs are allocated and installed by the IGP, so they are read from the
// kernel under the isis route protocol rather than from maeto's own SIDs.
func (t *topologyReporter) collect() (*nodesync.TopologyReport, error) {
	endX := dataplane.EncapTypeEndX
	isisProto := unix.RTPROT_ISIS

	sids, err := t.dp.GetSIDs(&endX, &isisProto)
	if err != nil {
		return nil, fmt.Errorf("list end.x sids: %w", err)
	}

	report := &nodesync.TopologyReport{
		NodeID:      t.nodeID,
		ReportedAt:  time.Now(),
		Adjacencies: make([]nodesync.AdjacencyReport, 0, len(sids)),
		Links:       t.links,
	}

	for _, sid := range sids {
		if sid.Dst == nil {
			continue
		}

		addr, ok := netip.AddrFromSlice(sid.Dst.IP)
		if !ok {
			continue
		}

		report.Adjacencies = append(report.Adjacencies, nodesync.AdjacencyReport{
			Interface: sid.Dev,
			EndX:      addr.Unmap(),
		})
	}

	return report, nil
}
