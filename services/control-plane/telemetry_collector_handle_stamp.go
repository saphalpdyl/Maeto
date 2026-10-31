package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/libs/store"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

const (
	// a late or redelivered probe this close behind the newest one is not a restart
	stampReorderTolerance = 64
	// past this many lost in a row the loss average is already pinned near 1
	stampMaxLostPerSample = 200
)

func (t *TelemetryCollector) handleSTAMP(
	ctx context.Context,
	resultPayload probe.ResultPacket,
	telemetryKey string,
) error {
	var stampPayload probe.STAMPResult
	if err := json.Unmarshal(resultPayload.Data, &stampPayload); err != nil {
		t.logger.ErrorContext(ctx, "failed to unmarshal stamp result", log.Err(err))
		return err
	}

	if stampPayload.IsSender {
		return nil
	}

	if stampPayload.SenderSequence == nil || stampPayload.SenderTimestamp == nil || stampPayload.ReceiveTimestamp == nil {
		t.logger.ErrorContext(ctx, "stamp result is missing sequence or timestamps")
		return errors.New("stamp result is missing sequence or timestamps")
	}

	duration := stampPayload.ReceiveTimestamp.Sub(*stampPayload.SenderTimestamp)
	if duration < 0 {
		t.logger.ErrorContext(ctx, "duration is negative")
		return errors.New("duration is negative")
	}

	durationMs := float64(duration.Microseconds()) / 1000
	seq := *stampPayload.SenderSequence
	edgeID := EdgeID(telemetryKey)

	t.mu.Lock()

	ewmas, ok := t.ewmas[edgeID]
	if !ok {
		ewmas = make(map[CostDimension]*Ewma)
		ewmas[COSTDIM_LATENCY] = &Ewma{
			config: EwmaConfig{
				Alpha:   0.3,
				Epsilon: 0.000001,
			},
		}

		ewmas[COSTDIM_LOSS] = &Ewma{
			config: EwmaConfig{
				Alpha:   0.05,
				Epsilon: 0.000001,
			},
		}

		t.ewmas[edgeID] = ewmas
	}

	observationTime := time.Now()
	ewmas[COSTDIM_LATENCY].Observe(observationTime, durationMs)

	lastSeq, seen := t.stampLastSeq[edgeID]
	switch {
	case !seen:
		ewmas[COSTDIM_LOSS].Observe(observationTime, 0)
		t.stampLastSeq[edgeID] = seq
	case seq > lastSeq:
		lost := min(seq-lastSeq-1, stampMaxLostPerSample)
		for range lost {
			ewmas[COSTDIM_LOSS].Observe(observationTime, 1)
		}
		ewmas[COSTDIM_LOSS].Observe(observationTime, 0)
		t.stampLastSeq[edgeID] = seq
	case lastSeq-seq > stampReorderTolerance:
		// the sender restarted and its sequence began again from zero
		ewmas[COSTDIM_LOSS].Observe(observationTime, 0)
		t.stampLastSeq[edgeID] = seq
	}

	latency := ewmas[COSTDIM_LATENCY].Snapshot()
	loss := ewmas[COSTDIM_LOSS].Mean()

	t.mu.Unlock()

	err := t.db.InTx(ctx, func(q *store.Queries) error {
		return q.InsertLinkSample(ctx, store.InsertLinkSampleParams{
			Time: pgtype.Timestamptz{
				Time:  observationTime,
				Valid: true,
			},
			TelemetryKey: telemetryKey,
			Sequence:     int64(seq),
			LatencyMs: pgtype.Float8{
				Float64: durationMs,
				Valid:   true,
			},
		})
	})

	if err != nil {
		t.logger.ErrorContext(ctx, "failed to insert link samples", log.Err(err))
		return err
	}

	delayNormalized, jitterNormalized, lossNormalized := NormalizeDelayJitterLoss(
		latency.Mean,
		latency.Variance,
		loss,
		100,
		10,
		0.05,
	)

	t.costGraph.UpdateCost(edgeID, COSTDIM_LATENCY, delayNormalized)
	t.costGraph.UpdateCost(edgeID, COSTDIM_JITTER, jitterNormalized)
	t.costGraph.UpdateCost(edgeID, COSTDIM_LOSS, lossNormalized)

	t.costGraph.MarkSeen(edgeID, time.Now())

	return nil
}

// NormalizeDelayJitterLoss scales delay, jitter and loss against reference values
// into costs that add up along a path: delay as is, jitter as variance (variances
// of independent links sum) and loss as -log(1-p) (survival probabilities multiply).
// lossReference is a plain probability, not log-transformed.
func NormalizeDelayJitterLoss(
	delayMs float64, // [0, inf)
	jitterVarianceMs2 float64, // [0, inf)
	loss float64, // [0, 1]
	delayReferenceMs float64,
	jitterReferenceMs float64,
	lossReference float64,
) (float64, float64, float64) {
	lossRefLog := -math.Log1p(-lossReference)

	delayNormalized := delayMs / delayReferenceMs
	jitterNormalized := jitterVarianceMs2 / (jitterReferenceMs * jitterReferenceMs)
	lossNormalized := -math.Log1p(-math.Min(loss, 0.999999)) / lossRefLog

	return delayNormalized, jitterNormalized, lossNormalized
}
