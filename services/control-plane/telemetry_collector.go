// Package controlplane telemetry_collector defines telemetry collector for each P-Router
package controlplane

import (
	"context"
	"fmt"
	"log/slog"
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

	SubjectPrefix string
	ForNode       NodeID
}

func NewTelemetryCollector(
	js jetstream.JetStream,
	logger *slog.Logger,
	forNode NodeID, // Eg: A
	subjectPrefix string, // Eg: maeto.probe.result.A.*
) *TelemetryCollector {
	return &TelemetryCollector{
		js:            js,
		logger:        logger,
		SubjectPrefix: subjectPrefix,
		ForNode:       forNode,
	}
}

// ensure owns the consumer only. The stream is shared by every node, so the
// controller creates it -- a collector doing so would bind the stream to its
// own node's subtree and cut off every other collector.
func (t *TelemetryCollector) ensure(ctx context.Context) error {
	cons, err := t.js.CreateOrUpdateConsumer(ctx, probe.ProbeResultStream, jetstream.ConsumerConfig{
		Durable:       fmt.Sprintf("telemetry-collector.%s", t.ForNode),
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
		t.logger.InfoContext(ctx, "received probe result",
			log.NodeID(string(t.ForNode)),
			slog.String("subject", msg.Subject()),
			slog.String("payload", string(msg.Data())),
		)

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
