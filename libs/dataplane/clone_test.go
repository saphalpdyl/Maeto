package dataplane_test

import (
	"net/netip"
	"testing"

	"github.com/saphalpdyl/maeto/libs/dataplane"
)

func TestNodeIntentCloneIsDeep(t *testing.T) {
	orig := &dataplane.NodeIntent{
		NodeType:   dataplane.NodeTypePE,
		Generation: 4,
		Intent: &dataplane.PEIntent{
			NodeID: "A",
			Tenants: map[string]map[string]dataplane.PE_PortalIntent{
				"273": {"48522cc7549b": {TunnelInterfaceID: 3}},
			},
		},
	}

	clone := orig.Clone()

	// mutate every level of the original
	orig.Generation = 99
	pe := orig.Intent.(*dataplane.PEIntent) // nolint:errcheck
	pe.NodeID = "B"
	pe.Tenants["273"]["48522cc7549b"] = dataplane.PE_PortalIntent{TunnelInterfaceID: 99}
	pe.Tenants["273"]["new-portal"] = dataplane.PE_PortalIntent{}
	pe.Tenants["999"] = map[string]dataplane.PE_PortalIntent{}

	got := clone.Intent.(*dataplane.PEIntent) // nolint:errcheck

	if clone.Generation != 4 {
		t.Errorf("Generation = %d, want 4", clone.Generation)
	}
	if got.NodeID != "A" {
		t.Errorf("NodeID = %q, want %q", got.NodeID, "A")
	}
	if n := len(got.Tenants); n != 1 {
		t.Errorf("outer map has %d tenants, want 1 -- outer map is shared", n)
	}
	if n := len(got.Tenants["273"]); n != 1 {
		t.Errorf("inner map has %d portals, want 1 -- inner map is shared", n)
	}
	if id := got.Tenants["273"]["48522cc7549b"].TunnelInterfaceID; id != 3 {
		t.Errorf("TunnelInterfaceID = %d, want 3 -- value is shared", id)
	}
	if orig.Intent == clone.Intent {
		t.Error("clone shares the Intent pointer with the original")
	}
}

func TestCPEIntentClone(t *testing.T) {
	orig := &dataplane.CPEIntent{
		TunnelInterfaceID: 1,
		TunnelPE:          "PopA",
		SitePrefix:        netip.MustParsePrefix("fd7a:3921:111:1::/64"),
	}

	clone := orig.Clone().(*dataplane.CPEIntent) // nolint:errcheck
	orig.TunnelInterfaceID = 9
	orig.TunnelPE = "PopC"

	if clone.TunnelInterfaceID != 1 || clone.TunnelPE != "PopA" {
		t.Errorf("clone changed with the original: %+v", clone)
	}
}

func TestCloneNilSafe(t *testing.T) {
	var n *dataplane.NodeIntent
	if n.Clone() != nil {
		t.Error("nil NodeIntent should clone to nil")
	}

	empty := (&dataplane.NodeIntent{NodeType: dataplane.NodeTypePE}).Clone()
	if empty.Intent != nil {
		t.Error("a nil Intent should stay nil")
	}
}
