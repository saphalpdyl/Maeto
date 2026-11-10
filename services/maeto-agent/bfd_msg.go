package maetoagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"
)

// Sent by frr-modules/maeto_bfd_notif_sender.c. A connection starts with a snapshot.

const bfdMessageVersion = 1

type BFDMessageType string

const (
	BFDMessageSnapshot BFDMessageType = "snapshot"
	BFDMessageDelta    BFDMessageType = "delta"

	// Emitted by BFDListener, never by bfdd.
	BFDMessageDisconnected BFDMessageType = "disconnected"
)

type BFDState string

const (
	BFDStateAdminDown BFDState = "admin-down"
	BFDStateDown      BFDState = "down"
	BFDStateInit      BFDState = "init"
	BFDStateUp        BFDState = "up"
)

func (s BFDState) valid() bool {
	switch s {
	case BFDStateAdminDown, BFDStateDown, BFDStateInit, BFDStateUp:
		return true
	}
	return false
}

// The frr-bfdd YANG list keys.
type BFDSessionKey struct {
	Multihop   bool
	DestAddr   netip.Addr
	SourceAddr netip.Addr // multihop only
	Interface  string     // single-hop only
	VRF        string
}

type BFDSession struct {
	Key         BFDSessionKey
	SourceAddr  netip.Addr
	State       BFDState
	Diagnostic  string // empty when bfdd did not report one
	LocalDiscr  uint32
	RemoteDiscr uint32 // 0 when not learned yet
	LastChange  time.Time
}

type BFDMessage struct {
	Type     BFDMessageType
	Sessions []BFDSession
}

type bfdEnvelope struct {
	Type BFDMessageType  `json:"type"`
	V    int             `json:"v"`
	Data json.RawMessage `json:"data"`
}

func ParseBFDMessage(b []byte) (BFDMessage, error) {
	var env bfdEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return BFDMessage{}, fmt.Errorf("bfd: decode envelope: %w", err)
	}
	if env.V != bfdMessageVersion {
		return BFDMessage{}, fmt.Errorf("bfd: unsupported message version %d", env.V)
	}
	if len(env.Data) == 0 {
		return BFDMessage{}, errors.New("bfd: message has no data")
	}

	var (
		sessions []BFDSession
		err      error
	)
	switch env.Type {
	case BFDMessageSnapshot:
		sessions, err = parseBFDSnapshot(env.Data)
	case BFDMessageDelta:
		var s BFDSession
		s, err = parseBFDDelta(env.Data)
		sessions = []BFDSession{s}
	default:
		return BFDMessage{}, fmt.Errorf("bfd: unknown message type %q", env.Type)
	}
	if err != nil {
		return BFDMessage{}, err
	}
	return BFDMessage{Type: env.Type, Sessions: sessions}, nil
}

type bfdSnapshotData struct {
	BFDD struct {
		BFD struct {
			Sessions struct {
				SingleHop []bfdSnapshotSession `json:"single-hop"`
				MultiHop  []bfdSnapshotSession `json:"multi-hop"`
			} `json:"sessions"`
		} `json:"bfd"`
	} `json:"frr-bfdd:bfdd"`
}

type bfdSnapshotSession struct {
	DestAddr   string `json:"dest-addr"`
	SourceAddr string `json:"source-addr"`
	Interface  string `json:"interface"`
	VRF        string `json:"vrf"`
	Stats      struct {
		LocalDiscr   uint32   `json:"local-discriminator"`
		LocalState   BFDState `json:"local-state"`
		LocalDiag    string   `json:"local-diagnostic"`
		RemoteDiscr  uint32   `json:"remote-discriminator"`
		LastDownTime string   `json:"last-down-time"`
		LastUpTime   string   `json:"last-up-time"`
	} `json:"stats"`
}

func parseBFDSnapshot(data json.RawMessage) ([]BFDSession, error) {
	var d bfdSnapshotData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("bfd: decode snapshot: %w", err)
	}

	lists := d.BFDD.BFD.Sessions
	out := make([]BFDSession, 0, len(lists.SingleHop)+len(lists.MultiHop))
	for _, l := range []struct {
		multihop bool
		entries  []bfdSnapshotSession
	}{{false, lists.SingleHop}, {true, lists.MultiHop}} {
		for i, e := range l.entries {
			s, err := e.session(l.multihop)
			if err != nil {
				return nil, fmt.Errorf("bfd: snapshot session %d: %w", i, err)
			}
			out = append(out, s)
		}
	}
	return out, nil
}

func (e *bfdSnapshotSession) session(multihop bool) (BFDSession, error) {
	key, src, err := bfdKey(multihop, e.DestAddr, e.SourceAddr, e.Interface, e.VRF)
	if err != nil {
		return BFDSession{}, err
	}
	if !e.Stats.LocalState.valid() {
		return BFDSession{}, fmt.Errorf("invalid local-state %q", e.Stats.LocalState)
	}

	changed := e.Stats.LastDownTime
	if e.Stats.LocalState == BFDStateUp {
		changed = e.Stats.LastUpTime
	}

	return BFDSession{
		Key:         key,
		SourceAddr:  src,
		State:       e.Stats.LocalState,
		Diagnostic:  e.Stats.LocalDiag,
		LocalDiscr:  e.Stats.LocalDiscr,
		RemoteDiscr: e.Stats.RemoteDiscr,
		LastChange:  parseBFDTime(changed),
	}, nil
}

type bfdDeltaData struct {
	SingleHop *bfdNotification `json:"frr-bfdd:singlehop-notification"`
	MultiHop  *bfdNotification `json:"frr-bfdd:multihop-notification"`
}

type bfdNotification struct {
	LocalDiscr  uint32   `json:"local-discr"`
	RemoteDiscr uint32   `json:"remote-discr"`
	NewState    BFDState `json:"new-state"`
	Reason      string   `json:"state-change-reason"`
	ChangedAt   string   `json:"time-of-last-state-change"`
	DestAddr    string   `json:"dest-addr"`
	SourceAddr  string   `json:"source-addr"`
	VRF         string   `json:"vrf"`
	Interface   string   `json:"interface"`
}

func parseBFDDelta(data json.RawMessage) (BFDSession, error) {
	var d bfdDeltaData
	if err := json.Unmarshal(data, &d); err != nil {
		return BFDSession{}, fmt.Errorf("bfd: decode delta: %w", err)
	}

	var (
		n        *bfdNotification
		multihop bool
	)
	switch {
	case d.SingleHop != nil && d.MultiHop == nil:
		n = d.SingleHop
	case d.MultiHop != nil && d.SingleHop == nil:
		n, multihop = d.MultiHop, true
	default:
		return BFDSession{}, errors.New("bfd: delta must hold exactly one known notification")
	}

	key, src, err := bfdKey(multihop, n.DestAddr, n.SourceAddr, n.Interface, n.VRF)
	if err != nil {
		return BFDSession{}, fmt.Errorf("bfd: delta: %w", err)
	}
	if !n.NewState.valid() {
		return BFDSession{}, fmt.Errorf("bfd: delta: invalid new-state %q", n.NewState)
	}

	return BFDSession{
		Key:         key,
		SourceAddr:  src,
		State:       n.NewState,
		Diagnostic:  n.Reason,
		LocalDiscr:  n.LocalDiscr,
		RemoteDiscr: n.RemoteDiscr,
		LastChange:  parseBFDTime(n.ChangedAt),
	}, nil
}

func bfdKey(multihop bool, dest, source, ifname, vrf string) (BFDSessionKey, netip.Addr, error) {
	d, err := netip.ParseAddr(dest)
	if err != nil {
		return BFDSessionKey{}, netip.Addr{}, fmt.Errorf("dest-addr: %w", err)
	}

	var src netip.Addr
	if source != "" {
		if src, err = netip.ParseAddr(source); err != nil {
			return BFDSessionKey{}, netip.Addr{}, fmt.Errorf("source-addr: %w", err)
		}
	}

	key := BFDSessionKey{Multihop: multihop, DestAddr: d, VRF: vrf}
	if multihop {
		if !src.IsValid() {
			return BFDSessionKey{}, netip.Addr{}, errors.New("multihop session without source-addr")
		}
		key.SourceAddr = src
	} else {
		key.Interface = ifname
	}
	return key, src, nil
}

func parseBFDTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
