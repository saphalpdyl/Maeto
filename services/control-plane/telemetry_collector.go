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
	"github.com/saphalpdyl/maeto/libs/store"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

type TelemetryCollector struct {
	mu sync.Mutex

	db     *store.DataStore
	js     jetstream.JetStream
	cons   jetstream.Consumer
	cc     jetstream.ConsumeContext
	logger *slog.Logger

	inventory NodeInventory
	topology  *ClabTopologyManager
	costGraph *CostGraph

	SubjectPrefix string
	ForNode       NodeID

	ewmas map[EdgeID]map[CostDimension]*Ewma

	stampResultBuffer map[EdgeID]*ProbeFIFOBuffer[probe.STAMPResult]
}

func NewTelemetryCollector(
	js jetstream.JetStream,
	db *store.DataStore,
	logger *slog.Logger,
	forNode NodeID, // Eg: A
	subjectPrefix string, // Eg: maeto.probe.result.A.*
	inventory NodeInventory,
	topology *ClabTopologyManager,
	costGraph *CostGraph,
) *TelemetryCollector {

	return &TelemetryCollector{
		js:                js,
		logger:            logger,
		SubjectPrefix:     subjectPrefix,
		ForNode:           forNode,
		inventory:         inventory,
		topology:          topology,
		costGraph:         costGraph,
		ewmas:             make(map[EdgeID]map[CostDimension]*Ewma),
		db:                db,
		stampResultBuffer: make(map[EdgeID]*ProbeFIFOBuffer[probe.STAMPResult]),
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
	var resultPayload probe.ResultPacket
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

	switch resultPayload.ProbeType {
	case probe.ProbeTypeSTAMP:
		return t.handleSTAMP(
			ctx,
			resultPayload,
			telemetryKey,
		)
	case probe.ProbeTypeNetStat:
		return t.handleNetStat(ctx, resultPayload, publisherNode)
	default:
		t.logger.WarnContext(ctx, "unknown result probe type", slog.String("probe_type", string(resultPayload.ProbeType)))
		return errors.New("unknown result probe type")
	}
}

type ProbeFIFOBuffer[T probe.ProbeResult] struct {
	mu  sync.RWMutex
	buf []T

	bufferSize uint32

	// Ring buffer
	head  uint32
	count uint32
}

func NewProbeFIFOBuffer[T probe.ProbeResult](bufferSize uint32) (*ProbeFIFOBuffer[T], error) {

	if bufferSize == 0 {
		return nil, errors.New("buffer size must be greater than zero")
	}

	return &ProbeFIFOBuffer[T]{
		buf: make([]T, bufferSize),
		bufferSize: bufferSize,
	}, nil
}

func (p *ProbeFIFOBuffer[T]) Len() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.count
}

func (p *ProbeFIFOBuffer[T]) EnqueueWithDrop(v T) (error, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	dropped := false
	if p.count == uint32(len(p.buf)) {
		p.dequeue()
		dropped = true
	}

	p.enqueue(v)
	return nil, dropped
}

func (p *ProbeFIFOBuffer[T]) tail() uint32 {
	return (p.head + p.count) % uint32(len(p.buf))
}

func (p *ProbeFIFOBuffer[T]) enqueue(v T) {
	p.buf[p.tail()] = v
	p.count++
}

func (p *ProbeFIFOBuffer[T]) dequeue() T {
	var zero T
	if p.count == 0 {
		return zero
	}

	val := p.buf[p.head]
	p.buf[p.head] = zero
	p.head = (p.head + 1) % uint32(len(p.buf))
	p.count--

	return val
}

func (p *ProbeFIFOBuffer[T]) Snapshot() []T {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snapshot := make([]T, p.count)
	for i := uint32(0); i < p.count; i++ {
		snapshot[i] = p.buf[(p.head+i) % uint32(len(p.buf))]
	}

	return snapshot
}

func (p *ProbeFIFOBuffer[T]) IsFull() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.count == uint32(len(p.buf))
}
