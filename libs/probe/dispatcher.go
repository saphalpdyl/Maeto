// dispatcher sends collected telemetry to the delivery endpoint (NATS in our case)
package probe

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type Result struct {
	ProbeType    ProbeType       `json:"probe_type"`
	TelemetryKey string          `json:"telemetry_key"`
	SentAt       time.Time       `json:"sent_at"`
	Data         json.RawMessage `json:"data"` // Contains probe-specific data to be unraveled
}

type STAMPResult struct {
	IsSender bool       `json:"is_sender"`
	Sequence uint32     `json:"sequence"`
	Peer     netip.Addr `json:"peer"`

	SenderSequence   *uint32    `json:"sender_sequence,omitempty"`
	SenderTimestamp  *time.Time `json:"sender_timestamp,omitempty"`
	ReceiveTimestamp *time.Time `json:"receive_timestamp,omitempty"`
}

type Dispatcher interface {
	Dispatch(ctx context.Context, result Result) error
}

const (
	ProbeResultStream        = "PROBES"
	ProbeResultSubjectPrefix = "maeto.probe.result"

	defaultPublishTimeout = 5 * time.Second
)

type JetStreamPublisher interface {
	Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// The probe result stream is owned by the control plane, so a dispatcher only
// publishes into it and never creates it.
type NATSDispatcherConfig struct {
	SubjectPrefix  string
	PublishTimeout time.Duration
}

type NATSDispatcher struct {
	js  JetStreamPublisher
	cfg NATSDispatcherConfig
}

func NewNATSDispatcher(js JetStreamPublisher, cfg NATSDispatcherConfig) *NATSDispatcher {
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = ProbeResultSubjectPrefix
	}

	if cfg.PublishTimeout <= 0 {
		cfg.PublishTimeout = defaultPublishTimeout
	}

	return &NATSDispatcher{js: js, cfg: cfg}
}

func (d *NATSDispatcher) Subject(result Result) string {
	return fmt.Sprintf("%s.%s.%s",
		d.cfg.SubjectPrefix,
		result.ProbeType,
		result.TelemetryKey,
	)
}

func (d *NATSDispatcher) Dispatch(ctx context.Context, result Result) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal probe result %s: %w", result.TelemetryKey, err)
	}

	ctx, cancel := context.WithTimeout(ctx, d.cfg.PublishTimeout)
	defer cancel()

	_, err = d.js.Publish(ctx, d.Subject(result), payload, jetstream.WithMsgID(resultMsgID(result)))
	if err != nil {
		return fmt.Errorf("failed to publish probe result %s: %w", result.TelemetryKey, err)
	}

	return nil
}

func resultMsgID(result Result) string {
	sum := sha256.Sum256(result.Data)

	return fmt.Sprintf("%s.%s.%d.%x",
		result.ProbeType,
		result.TelemetryKey,
		result.SentAt.UnixNano(),
		sum[:8],
	)
}
