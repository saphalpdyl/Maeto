package maetoagent

import (
	"net/netip"
	"testing"
	"time"
)

// Captured from bfdd on a PoP.
const (
	testBFDSnapshot      = `{"type": "snapshot", "v":1, "data": {"frr-bfdd:bfdd":{"bfd":{"sessions":{"single-hop":[{"dest-addr":"fe80::a8c1:abff:fe2d:d84e","interface":"eth2","vrf":"default","stats":{"local-discriminator":1625618572,"local-state":"up","local-diagnostic":"ok","local-multiplier":6,"remote-discriminator":827534094,"remote-state":"up","remote-diagnostic":"ok","remote-multiplier":6,"negotiated-transmission-interval":50000,"negotiated-receive-interval":50000,"detection-mode":"async-without-echo","last-down-time":"2026-11-10T01:33:58+00:00","last-up-time":"2026-11-10T01:33:58+00:00","session-down-count":0,"session-up-count":1,"control-packet-input-count":"0","control-packet-input-count-bad":"0","control-packet-output-count":"0","negotiated-echo-transmission-interval":0,"echo-packet-input-count":"0","echo-packet-output-count":"0"}},{"dest-addr":"fe80::a8c1:abff:fedc:3fe4","interface":"eth3","vrf":"default","stats":{"local-discriminator":3051238595,"local-state":"up","local-diagnostic":"ok","local-multiplier":6,"remote-discriminator":1472860270,"remote-state":"up","remote-diagnostic":"ok","remote-multiplier":6,"negotiated-transmission-interval":50000,"negotiated-receive-interval":50000,"detection-mode":"async-without-echo","last-down-time":"2026-11-10T01:52:11+00:00","last-up-time":"2026-11-10T01:52:11+00:00","session-down-count":0,"session-up-count":1,"control-packet-input-count":"0","control-packet-input-count-bad":"0","control-packet-output-count":"0","negotiated-echo-transmission-interval":0,"echo-packet-input-count":"0","echo-packet-output-count":"0"}}]}}}}}`
	testBFDEmptySnapshot = `{"type": "snapshot", "v":1, "data": {"frr-bfdd:bfdd":{"bfd":{}}}}`
	testBFDDelta         = `{"type": "delta", "v":1, "data": {"frr-bfdd:singlehop-notification":{"local-discr":1208403000,"remote-discr":529399968,"new-state":"up","time-of-last-state-change":"2026-11-10T02:00:09+00:00","dest-addr":"fe80::a8c1:abff:fedc:3fe4","source-addr":"fe80::a8c1:abff:fe0b:ca6e","vrf":"default","interface":"eth3","echo-enabled":false}}}`
)

func TestParseBFDSnapshot(t *testing.T) {
	m, err := ParseBFDMessage([]byte(testBFDSnapshot))
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != BFDMessageSnapshot || len(m.Sessions) != 2 {
		t.Fatalf("got type %q with %d sessions", m.Type, len(m.Sessions))
	}

	s := m.Sessions[1]
	want := BFDSessionKey{
		DestAddr:  netip.MustParseAddr("fe80::a8c1:abff:fedc:3fe4"),
		Interface: "eth3",
		VRF:       "default",
	}
	if s.Key != want {
		t.Errorf("key = %+v, want %+v", s.Key, want)
	}
	if s.State != BFDStateUp || s.Diagnostic != "ok" ||
		s.LocalDiscr != 3051238595 || s.RemoteDiscr != 1472860270 {
		t.Errorf("unexpected session %+v", s)
	}
	if !s.LastChange.Equal(time.Date(2026, 11, 10, 1, 52, 11, 0, time.UTC)) {
		t.Errorf("LastChange = %v", s.LastChange)
	}
}

func TestParseBFDEmptySnapshot(t *testing.T) {
	m, err := ParseBFDMessage([]byte(testBFDEmptySnapshot))
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != BFDMessageSnapshot || len(m.Sessions) != 0 {
		t.Fatalf("got type %q with %d sessions", m.Type, len(m.Sessions))
	}
}

func TestParseBFDDelta(t *testing.T) {
	m, err := ParseBFDMessage([]byte(testBFDDelta))
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != BFDMessageDelta || len(m.Sessions) != 1 {
		t.Fatalf("got type %q with %d sessions", m.Type, len(m.Sessions))
	}

	s := m.Sessions[0]
	if s.State != BFDStateUp || s.LocalDiscr != 1208403000 || s.RemoteDiscr != 529399968 {
		t.Errorf("unexpected session %+v", s)
	}
	if s.SourceAddr != netip.MustParseAddr("fe80::a8c1:abff:fe0b:ca6e") {
		t.Errorf("SourceAddr = %v", s.SourceAddr)
	}

	// The delta must address the same session as the snapshot entry.
	snap, err := ParseBFDMessage([]byte(testBFDSnapshot))
	if err != nil {
		t.Fatal(err)
	}
	if s.Key != snap.Sessions[1].Key {
		t.Errorf("delta key %+v != snapshot key %+v", s.Key, snap.Sessions[1].Key)
	}
}

func TestParseBFDMultihopDelta(t *testing.T) {
	msg := `{"type":"delta","v":1,"data":{"frr-bfdd:multihop-notification":{"local-discr":1,"new-state":"down","state-change-reason":"control-expired","dest-addr":"2001:db8::2","source-addr":"2001:db8::1","vrf":"default"}}}`
	m, err := ParseBFDMessage([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	s := m.Sessions[0]
	if !s.Key.Multihop || s.Key.SourceAddr != netip.MustParseAddr("2001:db8::1") ||
		s.State != BFDStateDown || s.Diagnostic != "control-expired" || !s.LastChange.IsZero() {
		t.Errorf("unexpected session %+v", s)
	}
}

func TestParseBFDMessageErrors(t *testing.T) {
	for name, msg := range map[string]string{
		"not json":        `nope`,
		"wrong version":   `{"type":"delta","v":2,"data":{}}`,
		"unknown type":    `{"type":"hello","v":1,"data":{}}`,
		"no data":         `{"type":"delta","v":1}`,
		"no notif":        `{"type":"delta","v":1,"data":{}}`,
		"bad state":       `{"type":"delta","v":1,"data":{"frr-bfdd:singlehop-notification":{"new-state":"sideways","dest-addr":"fe80::1","vrf":"default"}}}`,
		"bad addr":        `{"type":"delta","v":1,"data":{"frr-bfdd:singlehop-notification":{"new-state":"up","dest-addr":"nope","vrf":"default"}}}`,
		"multihop no src": `{"type":"delta","v":1,"data":{"frr-bfdd:multihop-notification":{"new-state":"up","dest-addr":"2001:db8::2","vrf":"default"}}}`,
	} {
		if _, err := ParseBFDMessage([]byte(msg)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
