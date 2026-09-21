package nodesync_test

import (
	"net/netip"
	"testing"

	"github.com/saphalpdyl/maeto/libs/nodesync"
)

func TestNodeIntentCloneIsDeep(t *testing.T) {
	orig := &nodesync.NodeIntent{
		NodeType:   nodesync.NodeTypePE,
		Generation: 4,
		Intent: &nodesync.PEIntent{
			NodeID: "A",
			Tenants: map[string]*nodesync.TenantIntent{
				"273": {
					PortalIntents: map[string]nodesync.PE_PortalIntent{
						"48522cc7549b": {TunnelInterfaceID: 3},
					},
					DT46SID: netip.MustParseAddr("fc00:0:1:ff8c::"),
					InstallPaths: []nodesync.PESIDInstallIntent{
						{
							TenantID:     "273",
							PrefixRoutes: []netip.Prefix{netip.MustParsePrefix("fd7a:3921:111:2::/64")},
							Segments: []netip.Addr{
								netip.MustParseAddr("fc00:0:2::"),
								netip.MustParseAddr("fc00:0:3::"),
							},
							Color: 20,
						},
					},
				},
			},
			Peers: map[string]nodesync.PeerIntent{
				"B": {
					PeerLocator:   netip.MustParsePrefix("fc00:0:2::/48"),
					PeerInterface: "eth1",
					TelemetryKey:  "A:eth1-B:eth3",
				},
			},
		},
	}

	clone := orig.Clone()

	// mutate every level of the original
	orig.Generation = 99
	pe := orig.Intent.(*nodesync.PEIntent) // nolint:errcheck
	pe.NodeID = "B"
	pe.Tenants["273"].PortalIntents["48522cc7549b"] = nodesync.PE_PortalIntent{TunnelInterfaceID: 99}
	pe.Tenants["273"].PortalIntents["new-portal"] = nodesync.PE_PortalIntent{}
	pe.Tenants["273"].InstallPaths[0].Segments[0] = netip.MustParseAddr("fc00:0:9::")
	pe.Tenants["273"].InstallPaths[0].PrefixRoutes[0] = netip.MustParsePrefix("fd00::/64")
	pe.Tenants["273"].InstallPaths[0].Color = 99
	pe.Tenants["999"] = &nodesync.TenantIntent{}
	pe.Peers["B"] = nodesync.PeerIntent{TelemetryKey: "mutated"}
	pe.Peers["C"] = nodesync.PeerIntent{}

	got := clone.Intent.(*nodesync.PEIntent) // nolint:errcheck

	if clone.Generation != 4 {
		t.Errorf("Generation = %d, want 4", clone.Generation)
	}
	if got.NodeID != "A" {
		t.Errorf("NodeID = %q, want %q", got.NodeID, "A")
	}
	if n := len(got.Tenants); n != 1 {
		t.Errorf("outer map has %d tenants, want 1 -- outer map is shared", n)
	}
	if n := len(got.Tenants["273"].PortalIntents); n != 1 {
		t.Errorf("portal map has %d portals, want 1 -- portal map is shared", n)
	}
	if id := got.Tenants["273"].PortalIntents["48522cc7549b"].TunnelInterfaceID; id != 3 {
		t.Errorf("TunnelInterfaceID = %d, want 3 -- value is shared", id)
	}
	if sid := got.Tenants["273"].DT46SID; sid.String() != "fc00:0:1:ff8c::" {
		t.Errorf("DT46SID = %s, want fc00:0:1:ff8c:: -- dropped by the clone", sid)
	}
	installed := got.Tenants["273"].InstallPaths
	if len(installed) != 1 {
		t.Fatalf("InstallPaths has %d entries, want 1", len(installed))
	}
	if sid := installed[0].Segments[0].String(); sid != "fc00:0:2::" {
		t.Errorf("Segments[0] = %s, want fc00:0:2:: -- segment slice is shared", sid)
	}
	if route := installed[0].PrefixRoutes[0].String(); route != "fd7a:3921:111:2::/64" {
		t.Errorf("PrefixRoutes[0] = %s, want fd7a:3921:111:2::/64 -- prefix slice is shared", route)
	}
	if installed[0].Color != 20 {
		t.Errorf("Color = %d, want 20 -- entry is shared", installed[0].Color)
	}
	if n := len(got.Peers); n != 1 {
		t.Errorf("peer map has %d peers, want 1 -- peer map is shared", n)
	}
	if key := got.Peers["B"].TelemetryKey; key != "A:eth1-B:eth3" {
		t.Errorf("TelemetryKey = %q, want %q -- peer map is shared", key, "A:eth1-B:eth3")
	}
	if orig.Intent == clone.Intent {
		t.Error("clone shares the Intent pointer with the original")
	}
}

func TestCPEIntentClone(t *testing.T) {
	orig := &nodesync.CPEIntent{
		TunnelInterfaceID: 1,
		TunnelPE:          "PopA",
		SitePrefix:        netip.MustParsePrefix("fd7a:3921:111:1::/64"),
	}

	clone := orig.Clone().(*nodesync.CPEIntent) // nolint:errcheck
	orig.TunnelInterfaceID = 9
	orig.TunnelPE = "PopC"

	if clone.TunnelInterfaceID != 1 || clone.TunnelPE != "PopA" {
		t.Errorf("clone changed with the original: %+v", clone)
	}
}

func TestCloneNilSafe(t *testing.T) {
	var n *nodesync.NodeIntent
	if n.Clone() != nil {
		t.Error("nil NodeIntent should clone to nil")
	}

	empty := (&nodesync.NodeIntent{NodeType: nodesync.NodeTypePE}).Clone()
	if empty.Intent != nil {
		t.Error("a nil Intent should stay nil")
	}
}
