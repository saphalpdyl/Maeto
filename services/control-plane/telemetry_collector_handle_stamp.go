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

	t.mu.Lock()
	buffer, exists := t.stampResultBuffer[EdgeID(telemetryKey)]
	if !exists {
		var err error
		buffer, err = NewProbeFIFOBuffer[probe.STAMPResult](7)
		if err != nil {
			t.logger.ErrorContext(ctx, "failed to create buffer", log.Err(err))
			t.mu.Unlock()
			return err
		}

		t.stampResultBuffer[EdgeID(telemetryKey)] = buffer
	}

	err, _ := buffer.EnqueueWithDrop(stampPayload)
	if err != nil {
		t.logger.ErrorContext(ctx, "failed to enqueue stamp result", log.Err(err))
		t.mu.Unlock()
		return err
	}

	jitter := float64(-1)
	loss := float64(-1)

	if buffer.IsFull() {
		// Buffer saturated, can calculate loss and jitter
		bufferSnapshot := buffer.Snapshot()

		maxSeq := uint32(0)
		minSeq := uint32(math.MaxUint32)

		for i := 0; i < len(bufferSnapshot)-1; i++ {
			p1 := bufferSnapshot[i]
			p2 := bufferSnapshot[i+1]

			// loss
			if i == 0 {
				if *p1.SenderSequence > maxSeq {
					maxSeq = *p1.SenderSequence
				}
				if *p1.SenderSequence < minSeq {
					minSeq = *p1.SenderSequence
				}
			}

			if *p2.SenderSequence > maxSeq {
				maxSeq = *p2.SenderSequence
			}
			if *p2.SenderSequence < minSeq {
				minSeq = *p2.SenderSequence
			}

			// jitter
			d1 := p1.ReceiveTimestamp.Sub(*p1.SenderTimestamp)
			d2 := p2.ReceiveTimestamp.Sub(*p2.SenderTimestamp)

			d1Ms := float64(d1.Microseconds()) / 1000
			d2Ms := float64(d2.Microseconds()) / 1000

			jitter += math.Abs(d1Ms - d2Ms)
		}

		jitter /= float64(len(bufferSnapshot) - 1)

		// loss
		expectedPktsCount := maxSeq - minSeq + 1
		loss = 1 - float64(buffer.Len())/float64(expectedPktsCount)
	}

	ewmas, ok := t.ewmas[EdgeID(telemetryKey)]
	if !ok {
		ewmas = make(map[CostDimension]*Ewma)
		ewmas[COSTDIM_LATENCY] = &Ewma{
			config: EwmaConfig{
				Alpha:   0.3,
				Epsilon: 0.000001,
			},
		}

		ewmas[COSTDIM_JITTER] = &Ewma{
			config: EwmaConfig{
				Alpha:   0.1,
				Epsilon: 0.000001,
			},
		}

		ewmas[COSTDIM_LOSS] = &Ewma{
			config: EwmaConfig{
				Alpha:   0.05,
				Epsilon: 0.000001,
			},
		}

		t.ewmas[EdgeID(telemetryKey)] = ewmas
	}

	duration := stampPayload.ReceiveTimestamp.Sub(*stampPayload.SenderTimestamp)
	if duration < 0 {
		t.logger.ErrorContext(ctx, "duration is negative")
		t.mu.Unlock()
		return errors.New("duration is negative")
	}

	durationMs := float64(duration.Microseconds()) / 1000

	observationTime := time.Now()

	ewmas[COSTDIM_LATENCY].Observe(observationTime, durationMs)
	if jitter != -1 {
		ewmas[COSTDIM_JITTER].Observe(observationTime, jitter)
	}

	if loss != -1 {
		ewmas[COSTDIM_LOSS].Observe(observationTime, loss)
	}

	t.mu.Unlock()

	seq := int64(0)
	if stampPayload.SenderSequence != nil {
		seq = int64(*stampPayload.SenderSequence)
	}

	// write to db
	err = t.db.InTx(ctx, func(q *store.Queries) error {
		return q.InsertLinkSample(ctx, store.InsertLinkSampleParams{
			Time: pgtype.Timestamptz{
				Time:  observationTime,
				Valid: true,
			},
			TelemetryKey: telemetryKey,
			Sequence:     seq,
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

	t.costGraph.UpdateCost(EdgeID(telemetryKey), COSTDIM_LATENCY, ewmas[COSTDIM_LATENCY].Mean())

	if jitter != -1 {
		t.costGraph.UpdateCost(EdgeID(telemetryKey), COSTDIM_JITTER, ewmas[COSTDIM_JITTER].Mean())
	}

	if loss != -1 {
		t.costGraph.UpdateCost(EdgeID(telemetryKey), COSTDIM_LOSS, ewmas[COSTDIM_LOSS].Mean())
	}

	t.costGraph.MarkSeen(EdgeID(telemetryKey), time.Now())

	return nil
}
