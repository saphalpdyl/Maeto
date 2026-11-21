package dataplane

import (
	"fmt"
	"net/netip"
)

// BFD for PE and CPE runs over tunnels (XFRM interfaces)
// IS-IS manages the intra-core bfdd sessions, but for
// the access edge, the management belongs to us.
type BFDPeer struct {
	Profile   string
	VRF       string
	Interface string
	Peer      netip.Addr
}

func (B BFDPeer) ID() ID {
	return ID{
		Kind: KindBFDPeer,
		Key:  fmt.Sprintf("bfd_peer:%s.%s.%s.%s", B.Profile, B.VRF, B.Interface, B.Peer.String()),
	}
}

func (B BFDPeer) CompareTo(actual Resource) Action {
	return ActionNone
}

func (B BFDPeer) Type() string {
	return "bfd_peer"
}
