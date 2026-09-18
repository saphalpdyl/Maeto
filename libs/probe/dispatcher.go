// dispatcher sends collected telemetry to the delivery endpoint (NATS in our case)
package probe

import (
	"context"
	"net/netip"
	"time"
)

type Result struct {
	ProbeID      string     `json:"probe_id"`
	ProbeType    ProbeType  `json:"probe_type"`
	TelemetryKey string     `json:"telemetry_key"`
	Peer         netip.Addr `json:"peer"`
	Sequence     uint32     `json:"sequence"`
	SentAt       time.Time  `json:"sent_at"`
}

type Dispatcher interface {
	Dispatch(ctx context.Context, result Result) error
}
