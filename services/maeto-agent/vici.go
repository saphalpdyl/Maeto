package maetoagent

import (
	"crypto/x509"
	"fmt"
	"strconv"

	"github.com/strongswan/govici/vici"
)

type ChildSA struct {
	Name     string `vici:"name"`
	UniqueID uint32 `vici:"uniqueid"`
	ReqID    uint32 `vici:"reqid"`
	State    string `vici:"state"`
	Mode     string `vici:"mode"`
	Protocol string `vici:"protocol"`

	IfIDIn  string `vici:"if-id-in"`
	IfIDOut string `vici:"if-id-out"`

	EncrAlg     string `vici:"encr-alg"`
	EncrKeysize uint32 `vici:"encr-keysize"`
	IntegAlg    string `vici:"integ-alg"`

	BytesIn    uint64 `vici:"bytes-in"`
	PacketsIn  uint64 `vici:"packets-in"`
	BytesOut   uint64 `vici:"bytes-out"`
	PacketsOut uint64 `vici:"packets-out"`

	RekeyTime   uint64 `vici:"rekey-time"`
	LifeTime    uint64 `vici:"life-time"`
	InstallTime uint64 `vici:"install-time"`

	LocalTS  []string `vici:"local-ts"`
	RemoteTS []string `vici:"remote-ts"`
}

// vici renders if-id as hex, and iproute2 parses if_id with base 0, so the
// raw string must never reach `ip link` -- "00000010" would mean 8, not 16.
func (c *ChildSA) IfID() (uint32, error) {
	if c.IfIDIn != c.IfIDOut {
		return 0, fmt.Errorf("if-id-in %q != if-id-out %q", c.IfIDIn, c.IfIDOut)
	}

	id, err := strconv.ParseUint(c.IfIDIn, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("parse if-id %q: %w", c.IfIDIn, err)
	}

	// strongswan uses 0 for "no if_id", and the kernel rejects it on a link
	if id == 0 {
		return 0, fmt.Errorf("if-id is zero, no xfrm interface can bind it")
	}

	return uint32(id), nil
}

// IKESA is the connection-named section carried by both ike-updown and
// child-updown. Fields absent from a given event stay zero-valued.
type IKESA struct {
	UniqueID uint32 `vici:"uniqueid"`
	Version  uint8  `vici:"version"`
	State    string `vici:"state"`

	LocalHost string `vici:"local-host"`
	LocalPort uint16 `vici:"local-port"`
	LocalID   string `vici:"local-id"`
	LocalCert string `vici:"local-cert-data"` // raw DER

	RemoteHost string `vici:"remote-host"`
	RemotePort uint16 `vici:"remote-port"`
	RemoteID   string `vici:"remote-id"`
	RemoteCert string `vici:"remote-cert-data"` // raw DER

	InitiatorSPI string `vici:"initiator-spi"`
	ResponderSPI string `vici:"responder-spi"`

	EncrAlg     string `vici:"encr-alg"`
	EncrKeysize uint32 `vici:"encr-keysize"`
	IntegAlg    string `vici:"integ-alg"`
	PRFAlg      string `vici:"prf-alg"`
	DHGroup     string `vici:"dh-group"`

	Established uint64 `vici:"established"`
	RekeyTime   uint64 `vici:"rekey-time"`
	ReauthTime  uint64 `vici:"reauth-time"`

	TasksActive  []string `vici:"tasks-active"`
	TasksPassive []string `vici:"tasks-passive"`

	ChildSAs map[string]ChildSA `vici:"child-sas"` // e.g [cpe2]child_sas
}

// UpDownEvent is the decoded form of an ike-updown or child-updown event.
type UpDownEvent struct {
	Event string
	Up    bool
	Conn  string
	IKE   IKESA
}

func ParseUpDown(name string, m *vici.Message) (*UpDownEvent, error) {
	ev := &UpDownEvent{Event: name}

	if v, ok := m.Get("up").(string); ok && v == "yes" {
		ev.Up = true
	}

	for _, k := range m.Keys() {
		sub, ok := m.Get(k).(*vici.Message)
		if !ok {
			continue // "up" and any other kv pair
		}
		ev.Conn = k
		if err := vici.UnmarshalMessage(sub, &ev.IKE); err != nil {
			return nil, fmt.Errorf("unmarshal ike-sa %q: %w", k, err)
		}
		break // exactly one connection section per event
	}

	if ev.Conn == "" {
		return nil, fmt.Errorf("%s: no connection section in message", name)
	}
	return ev, nil
}

func (e *UpDownEvent) RemoteCertificate() (*x509.Certificate, error) {
	if e.IKE.RemoteCert == "" {
		return nil, fmt.Errorf("no remote-cert-data in event")
	}
	return x509.ParseCertificate([]byte(e.IKE.RemoteCert))
}
