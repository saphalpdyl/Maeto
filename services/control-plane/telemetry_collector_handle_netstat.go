package controlplane

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/libs/store"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

func (t *TelemetryCollector) handleNetStat(ctx context.Context, resultPayload probe.ResultPacket, publisherNode string) error {
	var netStatPayload probe.NetStatResult
	if err := json.Unmarshal(resultPayload.Data, &netStatPayload); err != nil {
		t.logger.ErrorContext(ctx, "failed to unmarshal net stat result", log.Err(err))
		return err
	}

	sampledAt := pgtype.Timestamptz{Time: resultPayload.SentAt, Valid: true}

	err := t.db.InTx(ctx, func(q *store.Queries) error {
		for iface, stats := range netStatPayload.Stats {
			err := q.InsertIfaceCounters(ctx, store.InsertIfaceCountersParams{
				Time:      sampledAt,
				NodeID:    publisherNode,
				Iface:     iface,
				RxBytes:   int64(stats["rx_bytes"]),
				TxBytes:   int64(stats["tx_bytes"]),
				RxPackets: int64(stats["rx_packets"]),
				TxPackets: int64(stats["tx_packets"]),
				RxDropped: int64(stats["rx_dropped"]),
				TxDropped: int64(stats["tx_dropped"]),
			})
			if err != nil {
				return fmt.Errorf("insert counters for %s: %w", iface, err)
			}
		}

		return nil
	})

	if err != nil {
		t.logger.ErrorContext(ctx, "failed to insert iface counters", log.Err(err))
		return err
	}

	return nil
}
