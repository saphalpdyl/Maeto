package probe

import (
	"fmt"
	"net/netip"
	"time"
)

type ProbeConfigSTAMP struct {
	ProbeType       ProbeType    `json:"probe_type"`
	PeerDestination netip.Prefix `json:"peer_destination"`
	TelemetryKey    string       `json:"telemetry_key"`
	IsSender        bool         `json:"is_sender"`
	NoReply         bool         `json:"no_reply"`
	Port            uint16       `json:"port"`
	BindToDev       string       `json:"bind_to_dev"`

	ProbeInterval time.Duration `json:"probe_interval"`
}

func (p *ProbeConfigSTAMP) GetID() string {
	role := "reflector"
	if p.IsSender {
		role = "sender"
		if p.NoReply {
			role = "sender-noreply"
		}
	}

	return fmt.Sprintf(
		"%s.%s.%s.%d.%s.%s.%s",
		p.ProbeType,
		role,
		p.PeerDestination.Addr().String(),
		stampPort(p),
		p.BindToDev,
		p.TelemetryKey,
		p.ProbeInterval.String(),
	)
}

func (p *ProbeConfigSTAMP) Validate() error {
	if p.ProbeType != ProbeTypeSTAMP {
		return fmt.Errorf("probe type must be %s, got %q", ProbeTypeSTAMP, p.ProbeType)
	}

	if !p.PeerDestination.Addr().IsValid() {
		return fmt.Errorf("invalid peer destination %q", p.PeerDestination)
	}

	if !p.IsSender {
		return nil
	}

	if !p.NoReply {
		return ErrReflectedUnsupported
	}

	if p.ProbeInterval <= 0 {
		return fmt.Errorf("probe interval must be positive, got %s", p.ProbeInterval)
	}

	return nil
}
