// Package controlplane telemetry_collector defines telemetry collector for each P-Router
package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/avast/retry-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

type TelemetryCollector struct {
	mu sync.Mutex

	js     jetstream.JetStream
	cons   jetstream.Consumer
	cc     jetstream.ConsumeContext
	logger *slog.Logger

	inventory NodeInventory
	topology  *ClabTopologyManager
	costGraph *CostGraph

	SubjectPrefix string
	ForNode       NodeID

	ewmas map[EdgeID]*Ewma
}

func NewTelemetryCollector(
	js jetstream.JetStream,
	logger *slog.Logger,
	forNode NodeID, // Eg: A
	subjectPrefix string, // Eg: maeto.probe.result.A.*
	inventory NodeInventory,
	topology *ClabTopologyManager,
	costGraph *CostGraph,
) *TelemetryCollector {
	return &TelemetryCollector{
		js:            js,
		logger:        logger,
		SubjectPrefix: subjectPrefix,
		ForNode:       forNode,
		inventory:     inventory,
		topology:      topology,
		costGraph:     costGraph,
		ewmas:         map[EdgeID]*Ewma{},
	}
}

// ensure owns the consumer only. The stream is shared by every node, so the
// controller creates it -- a collector doing so would bind the stream to its
// own node's subtree and cut off every other collector.
func (t *TelemetryCollector) ensure(ctx context.Context) error {
	cons, err := t.js.CreateOrUpdateConsumer(ctx, probe.ProbeResultStream, jetstream.ConsumerConfig{
		// no dots: nats treats the durable name as a single token, so a
		// separator that is also a subject separator is rejected outright
		Durable:       fmt.Sprintf("telemetry-collector-%s", t.ForNode),
		FilterSubject: t.SubjectPrefix + ".>",
		AckPolicy:     jetstream.AckExplicitPolicy,
	})

	if err != nil {
		return fmt.Errorf("failed to create consumer %s: %w", t.ForNode, err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.cons = cons

	return nil
}

func (t *TelemetryCollector) ensureWithRetry(ctx context.Context) error {
	err := retry.Do(func() error {
		err := t.ensure(ctx)
		if err != nil {
			return err
		}

		return nil
	}, retry.Attempts(3), retry.Delay(3*time.Second))

	if err != nil {
		t.logger.ErrorContext(ctx, "failed to ensure consumers", log.Err(err))
		return err
	}

	return nil
}

func (t *TelemetryCollector) Start(ctx context.Context) error {
	t.logger.InfoContext(ctx, "starting telemetry collector", log.NodeID(string(t.ForNode)))

	t.js.Conn().SetReconnectHandler(func(conn *nats.Conn) {
		t.logger.WarnContext(ctx, "nats reconnected, ensuring consumers")
		err := t.ensureWithRetry(ctx)
		if err != nil {
			t.logger.ErrorContext(ctx, "failed to ensure consumers", log.Err(err))
			return
		}
	})

	err := t.ensureWithRetry(ctx)
	if err != nil {
		return err
	}

	cc, err := t.cons.Consume(func(msg jetstream.Msg) {
		err := t.handleTelemetryConsume(ctx, msg)
		if err != nil {
			t.logger.ErrorContext(ctx, "failed to handle telemetry", log.Err(err))
			if err := msg.Nak(); err != nil {
				t.logger.ErrorContext(ctx, "failed to nak message", log.Err(err))
			}
			return
		}

		if err := msg.Ack(); err != nil {
			t.logger.ErrorContext(ctx, "failed to ack msg", log.Err(err))
		}
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer %s: %w", t.ForNode, err)
	}

	t.mu.Lock()
	t.cc = cc
	t.mu.Unlock()

	return nil
}

// Stop halts delivery to the consume callback. The durable consumer itself is
// left in place so a restart resumes from the last acked message.
func (t *TelemetryCollector) Stop() {
	t.mu.Lock()
	cc := t.cc
	t.cc = nil
	t.mu.Unlock()

	if cc != nil {
		cc.Stop()
	}
}

func (t *TelemetryCollector) handleTelemetryConsume(ctx context.Context, msg jetstream.Msg) error {
	var resultPayload probe.Result
	err := json.Unmarshal(msg.Data(), &resultPayload)
	if err != nil {
		t.logger.ErrorContext(ctx, "failed to unmarshal probe result", log.Err(err))
		return err
	}

	// TODO: Classify telemetry
	// maeto.probe.result.X.PROBE_TYPE.a:b-c:d
	subjectTokens := strings.Split(msg.Subject(), ".")
	prefixTokens := strings.Split(t.SubjectPrefix, ".")

	if len(subjectTokens) < len(prefixTokens)+2 {
		return fmt.Errorf("unexpected probe result subject %q", msg.Subject())
	}

	publisherNode := subjectTokens[len(prefixTokens)-1]
	telemetryKey := strings.Join(subjectTokens[len(prefixTokens)+1:], ".")

	if telemetryKey != resultPayload.TelemetryKey {
		return fmt.Errorf("telemetry key does not match the subject")
	}

	//edge, ok := t.topology.Graph().edges[EdgeID(telemetryKey)]
	//if !ok {
	//	t.logger.ErrorContext(ctx, fmt.Sprintf("telemetry key i.e the link: %s not found in graph", telemetryKey))
	//	return nil
	//}

	_ = publisherNode

	switch resultPayload.ProbeType {
	case probe.ProbeTypeSTAMP:
		var stampPayload probe.STAMPResult
		if err := json.Unmarshal(resultPayload.Data, &stampPayload); err != nil {
			t.logger.ErrorContext(ctx, "failed to unmarshal stamp result", log.Err(err))
			return err
		}

		if stampPayload.IsSender {
			return nil
		}

		t.mu.Lock()
		ewma, ok := t.ewmas[EdgeID(telemetryKey)]
		if !ok {
			ewma = &Ewma{
				config: EwmaConfig{
					Alpha:   0.3,
					Epsilon: 0.000001,
				},
			}

			t.ewmas[EdgeID(telemetryKey)] = ewma
		}

		duration := stampPayload.ReceiveTimestamp.Sub(*stampPayload.SenderTimestamp)
		if duration < 0 {
			t.logger.ErrorContext(ctx, "duration is negative")
			t.mu.Unlock()
			return errors.New("duration is negative")
		}
		ewma.Observe(time.Now(), float64(duration.Milliseconds()))
		t.mu.Unlock()

		t.costGraph.UpdateCost(EdgeID(telemetryKey), COSTDIM_LATENCY, ewma.Mean())

	default:
		t.logger.WarnContext(ctx, "unknown result probe type", slog.String("probe_type", string(resultPayload.ProbeType)))
		return errors.New("unknown result probe type")
	}

	return nil
}
