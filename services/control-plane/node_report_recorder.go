package controlplane

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/libs/store"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

const nodeReportRewatchDelay = 2 * time.Second

type NodeReportRecorder struct {
	js     jetstream.JetStream
	db     *store.DataStore
	logger *slog.Logger
}

func NewNodeReportRecorder(js jetstream.JetStream, db *store.DataStore, logger *slog.Logger) *NodeReportRecorder {
	return &NodeReportRecorder{
		js:     js,
		db:     db,
		logger: logger,
	}
}

func (n *NodeReportRecorder) Run(ctx context.Context) {
	feed := make(chan dataplane.NodeState, 32)
	go n.watch(ctx, feed)

	for {
		select {
		case <-ctx.Done():
			return
		case report := <-feed:
			n.record(ctx, report)
		}
	}
}

func (n *NodeReportRecorder) watch(ctx context.Context, feed chan<- dataplane.NodeState) {
	key := nodesync.Key("*", "*", nodesync.LaneDataplane)

	for ctx.Err() == nil {
		err := nodesync.Watch(ctx, n.js, n.logger, nodesync.NodeStateBucket, key, feed)
		if err != nil {
			n.logger.ErrorContext(ctx, "node report watch failed; rewatching", log.Err(err))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(nodeReportRewatchDelay):
		}
	}
}

func (n *NodeReportRecorder) record(ctx context.Context, report dataplane.NodeState) {
	if report.NodeID == "" || report.ReportedAt.IsZero() {
		return
	}

	err := n.db.Queries.InsertNodeReport(ctx, store.InsertNodeReportParams{
		Time:       pgtype.Timestamptz{Time: report.ReportedAt, Valid: true},
		NodeID:     report.NodeID,
		NodeType:   string(report.NodeType),
		Generation: int64(report.Generation),
		Observed:   report.Observed,
		Converged:  report.Converged,
		Passes:     int32(report.Passes),
		Error:      report.Error,
	})
	if err != nil {
		n.logger.ErrorContext(ctx, "failed to insert node report",
			slog.String("node_id", report.NodeID),
			log.Err(err),
		)
	}
}
