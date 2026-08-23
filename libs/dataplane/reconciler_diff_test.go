package dataplane_test

import (
	"log/slog"
	"net/netip"
	"sort"
	"testing"

	"github.com/saphalpdyl/maeto/libs/dataplane"
)

// A realistic cpe: tenant 273, site fd7a:3921:111:1::/64, homed to PopA at
// fd00:0:0:100::1, reached through the transit router at fd00:0:0:102::1.
const (
	tunIface = "xfrm-cust-273"
	peA      = "fd00:0:0:100::1/128"
	peC      = "fd00:0:0:300::1/128"
	accessGW = "fd00:0:0:102::1"
)

func mkXFRM(name string, ifID uint32) *dataplane.XFRM {
	return &dataplane.XFRM{Name: name, IfID: ifID, Parent: "lo", MasterVRF: ""}
}

func mkRoute(table int, dst, dev, via string) *dataplane.Route { // nolint:unparam
	r := &dataplane.Route{Table: table, Dst: netip.MustParsePrefix(dst), Dev: dev}
	if via != "" {
		r.Via = netip.MustParseAddr(via)
	}
	return r
}

func mkSet(rs ...dataplane.Resource) map[string]dataplane.Resource {
	out := make(map[string]dataplane.Resource, len(rs))
	for _, r := range rs {
		out[r.ID().Key] = r
	}
	return out
}

// the steady state a cpe should be in
func cpeSteadyList(ifID uint32, peEndpoint string) []dataplane.Resource {
	return []dataplane.Resource{
		mkXFRM(tunIface, ifID),
		mkRoute(0, peEndpoint, "eth1", accessGW), // esp exclude, so the tunnel can carry itself
		mkRoute(0, "::/0", tunIface, ""),         // everything else down the tunnel
	}
}

func cpeSteady(ifID uint32, peEndpoint string) map[string]dataplane.Resource {
	return mkSet(cpeSteadyList(ifID, peEndpoint)...)
}

func keysOf(rs []dataplane.Resource) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, string(r.ID().Kind)+":"+r.ID().Key)
	}
	sort.Strings(out)
	return out
}

// compares by identity, so the test does not depend on how ID().Key is encoded
func equalResources(got, want []dataplane.Resource) bool {
	g, w := keysOf(got), keysOf(want)
	if len(g) != len(w) {
		return false
	}
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

func TestReconcilerDiff(t *testing.T) {
	r := dataplane.NewReconciler(
		&TestDefaultRouteOnlyDataplane{},
		dataplane.NodeTypeCPE,
		slog.Default(),
		make(chan *dataplane.NodeIntent, 1),
	)

	cases := []struct {
		name       string
		current    map[string]dataplane.Resource
		desired    map[string]dataplane.Resource
		wantRemove []dataplane.Resource
		wantAdd    []dataplane.Resource
	}{
		{
			name:    "bootstrap: nothing installed yet",
			current: mkSet(),
			desired: cpeSteady(3, peA),
			wantAdd: cpeSteadyList(3, peA),
		},
		{
			name:    "steady state: nothing to do",
			current: cpeSteady(3, peA),
			desired: cpeSteady(3, peA),
		},
		{
			name:       "site re-homed to a different pe",
			current:    cpeSteady(3, peA),
			desired:    cpeSteady(3, peC),
			wantRemove: []dataplane.Resource{mkRoute(0, peA, "eth1", accessGW)},
			wantAdd:    []dataplane.Resource{mkRoute(0, peC, "eth1", accessGW)},
		},
		{
			name:    "someone deleted the tunnel default by hand",
			current: mkSet(mkXFRM(tunIface, 3), mkRoute(0, peA, "eth1", accessGW)),
			desired: cpeSteady(3, peA),
			wantAdd: []dataplane.Resource{mkRoute(0, "::/0", tunIface, "")},
		},
		{
			name: "stale interface from an earlier if_id",
			current: mkSet(
				mkXFRM(tunIface, 3),
				mkXFRM("xfrm-cust-999", 9), // orphan
				mkRoute(0, peA, "eth1", accessGW),
				mkRoute(0, "::/0", tunIface, ""),
			),
			desired:    cpeSteady(3, peA),
			wantRemove: []dataplane.Resource{mkXFRM("xfrm-cust-999", 9)},
		},
		{
			// charon reallocated if_id on reconnect. The name is stable, so the
			// key is identical -- but the interface must still be recreated,
			// because if_id cannot be changed on a live link.
			name:       "if_id changed, name unchanged",
			current:    cpeSteady(3, peA),
			desired:    cpeSteady(7, peA),
			wantRemove: []dataplane.Resource{mkXFRM(tunIface, 3)},
			wantAdd:    []dataplane.Resource{mkXFRM(tunIface, 7)},
		},
		{
			// same key (table|dst), but the route points somewhere else
			name:       "tunnel default points at a stale device",
			current:    mkSet(mkRoute(0, "::/0", "xfrm-cust-OLD", "")),
			desired:    mkSet(mkRoute(0, "::/0", tunIface, "")),
			wantRemove: []dataplane.Resource{mkRoute(0, "::/0", "xfrm-cust-OLD", "")},
			wantAdd:    []dataplane.Resource{mkRoute(0, "::/0", tunIface, "")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.Diff(t.Context(), tc.current, tc.desired)
			if err != nil {
				t.Fatalf("Diff returned error: %v", err)
			}

			if !equalResources(got.Remove, tc.wantRemove) {
				t.Errorf("Remove = %v, want %v", keysOf(got.Remove), keysOf(tc.wantRemove))
			}
			if !equalResources(got.Add, tc.wantAdd) {
				t.Errorf("Add = %v, want %v", keysOf(got.Add), keysOf(tc.wantAdd))
			}

			t.Logf("\n remove = %v \n add = %v \n want = %v \n", keysOf(got.Remove), keysOf(got.Add), keysOf(tc.wantAdd))
		})
	}
}
