package probe

import (
	"fmt"
	"net/netip"
	"time"
)

type ProbeConfigSTAMP struct {
	ProbeType       ProbeType     `json:"probe_type"`
	LocalLoopback   *netip.Prefix `json:"local_loopback"`
	PeerDestination netip.Addr    `json:"peer_destination"`
	TelemetryKey    string        `json:"telemetry_key"`
	IsSender        bool          `json:"is_sender"`
	NoReply         bool          `json:"no_reply"`
	DestPort        uint16        `json:"dest_port"`
	EgressInterface string        `json:"egress_interface"`
	EncapSegments   []netip.Addr  `json:"encap_segments"`

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
		p.PeerDestination.String(),
		stampPort(p),
		p.EgressInterface,
		p.TelemetryKey,
		p.ProbeInterval.String(),
	)
}

func (p *ProbeConfigSTAMP) Validate() error {
	if p.ProbeType != ProbeTypeSTAMP {
		return fmt.Errorf("probe type must be %s, got %q", ProbeTypeSTAMP, p.ProbeType)
	}

	if p.IsSender && !p.PeerDestination.IsValid() {
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
