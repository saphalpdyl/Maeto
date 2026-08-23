package dataplane_test

import (
	"net/netip"
	"os"
	"testing"

	"github.com/saphalpdyl/maeto/libs/dataplane"
)

func TestLiveDataplaneRead(t *testing.T) {
	if os.Getenv("MAETO_LIVE_DP") == "" {
		t.Skip("needs a real fib and root; set MAETO_LIVE_DP=1")
	}

	// testIntentFeed := make(chan *dataplane.NodeIntent, 32)

	dp := dataplane.NewLinuxNetlink()
	// r := dataplane.NewReconciler(dp, slog.Default(), testIntentFeed)

	// Testing all dataplane functions

	dRoute, dDev, err := dp.GetDefaultRouteAndDev()
	if err != nil {
		t.Fatalf("GetDefaultRouteAndDev failed: %v", err)
	}

	vrfLinks, err := dp.GetLinksByType("vrf")
	if err != nil {
		t.Fatalf("GetLinksByType(vrf) failed: %v", err)
	}

	xfrmLinks, err := dp.GetLinksByType("xfrm")
	if err != nil {
		t.Fatalf("GetLinksByType(xfrm) failed: %v", err)
	}

	t.Logf("Default route: %s, Default device: %s", dRoute, dDev)
	for _, l := range vrfLinks {
		t.Logf("VRF link: %+v", l)
	}

	for _, l := range xfrmLinks {
		t.Logf("XFRM link: %+v", l)
	}
}

type TestDefaultRouteOnlyDataplane struct {
}

func (t *TestDefaultRouteOnlyDataplane) GetDefaultRouteAndDev() (string, string, error) {
	return "fd00:0:0:100::1", "eth1", nil
}

func (t *TestDefaultRouteOnlyDataplane) GetLinksByType(ifaceType string) ([]dataplane.DataplaneLink, error) {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) GetPrefixRoutes() ([]dataplane.DataplaneRoute, error) {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) InsertPrefixRoute(tunnelIface string, tableID int, prefix netip.Prefix, via netip.Addr) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) InsertXFRMInterface(interfaceName string, underLayIface string, ifID uint32, masterVRFIndex *string) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) RemovePrefixRoute(prefix netip.Prefix, tableID int) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) RemoveVRF(linkIndex int) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) RemoveXFRMInterface(linkIndex int) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) UpsertPolicy(nhid string, dtsid string, vrfTableId string, vrfTableName string, sids []string) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) UpsertRouteToPolicy(dest string, vrfTableId string, nhid string) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) UpsertRule(priority int, src netip.Prefix, tableID int) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) RemoveRule(priority int, src netip.Prefix, tableID int) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) GetRules() ([]dataplane.DataplaneRule, error) {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) UpsertVRF(tableName string, tableID int) error {
	panic("unimplemented")
}

var _ dataplane.Dataplane = (*TestDefaultRouteOnlyDataplane)(nil)
