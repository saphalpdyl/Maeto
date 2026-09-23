// dispatcher sends collected telemetry to the delivery endpoint (NATS in our case)
package probe

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
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

type ToLogsDispatcher struct {
	logger *slog.Logger
}

func NewToLogsDispatcher(logger *slog.Logger) *ToLogsDispatcher {
	return &ToLogsDispatcher{
		logger: logger,
	}
}

func (t *ToLogsDispatcher) Dispatch(ctx context.Context, result Result) error {
	t.logger.InfoContext(ctx, "got result", slog.Any("result", result))
	return nil
}

const (
	ProbeResultStream        = "PROBES"
	ProbeResultSubjectPrefix = "maeto.probe.result"

	defaultPublishTimeout = 5 * time.Second
	defaultResultMaxAge   = 24 * time.Hour
)

type JetStreamPublisher interface {
	Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
	CreateOrUpdateStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error)
}

type NATSDispatcherConfig struct {
	StreamName     string
	SubjectPrefix  string
	MaxAge         time.Duration
	PublishTimeout time.Duration
}

type NATSDispatcher struct {
	js  JetStreamPublisher
	cfg NATSDispatcherConfig
}

func NewNATSDispatcher(js JetStreamPublisher, cfg NATSDispatcherConfig) *NATSDispatcher {
	if cfg.StreamName == "" {
		cfg.StreamName = ProbeResultStream
	}

	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = ProbeResultSubjectPrefix
	}

	if cfg.MaxAge <= 0 {
		cfg.MaxAge = defaultResultMaxAge
	}

	if cfg.PublishTimeout <= 0 {
		cfg.PublishTimeout = defaultPublishTimeout
	}

	return &NATSDispatcher{js: js, cfg: cfg}
}

func (d *NATSDispatcher) Ensure(ctx context.Context) error {
	_, err := d.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        d.cfg.StreamName,
		Description: "raw probe results published by maeto agents",
		Subjects:    []string{d.cfg.SubjectPrefix + ".>"},
		Storage:     jetstream.FileStorage,
		MaxAge:      d.cfg.MaxAge,
	})
	if err != nil {
		return fmt.Errorf("failed to create stream %s: %w", d.cfg.StreamName, err)
	}

	return nil
}

func (d *NATSDispatcher) Subject(result Result) string {
	return fmt.Sprintf("%s.%s.%s",
		d.cfg.SubjectPrefix,
		subjectToken(string(result.ProbeType)),
		subjectToken(result.TelemetryKey),
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
		subjectToken(string(result.ProbeType)),
		subjectToken(result.TelemetryKey),
		result.SentAt.UnixNano(),
		sum[:8],
	)
}

func subjectToken(raw string) string {
	if raw == "" {
		return "none"
	}

	return strings.ToLower(strings.Map(func(r rune) rune {
		switch r {
		case '.', '*', '>', ' ', '\t', '\n', '\r':
			return '_'
		}

		return r
	}, raw))
}
