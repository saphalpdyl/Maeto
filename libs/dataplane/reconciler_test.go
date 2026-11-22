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

	// testIntentFeed := make(chan *intent.NodeIntent, 32)

	dp, err := dataplane.NewLinuxNetlink(dataplane.DefaultBfddVTYSocketPath)
	if err != nil {
		t.Fatalf("failed to create linux dataplane: %v", err)
	}
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

func (t *TestDefaultRouteOnlyDataplane) StoreTriggers(egress string, rules []dataplane.TriggerRule) {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) RemoveTriggers(egress string) {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) GetTriggers() ([]dataplane.DataplaneRuleTrigger, error) {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) CleanupTriggers() error {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) ActivateRepair(egress string) error {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) ResetRepair(egress string, all bool) error {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) UpsertDTSID(sid netip.Addr, vrfTableID int, tableID int, sidType dataplane.EncapType) error {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) UpsertEndSID(sid netip.Addr, tableID int) error {
	//TODO implement me
	panic("implement me")
}

// RemoveRule implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) RemoveRule(priority int, src *netip.Prefix, tableID int, mark int, iifName string, dst *netip.Prefix) error {
	panic("unimplemented")
}

// UpsertRule implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) UpsertRule(priority int, src *netip.Prefix, tableID int, mark int, iifName string, dst *netip.Prefix) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) EnsureClassificationBase() error {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) InsertClassification(classifier uint32, mark uint32) error {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) GetClassifications() ([]dataplane.DataplaneClassification, error) {
	//TODO implement me
	panic("implement me")
}

func (t *TestDefaultRouteOnlyDataplane) RemoveClassification(classifier uint32) error {
	//TODO implement me
	panic("implement me")
}

// GetSRRoutes implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) GetSRRoutes() ([]dataplane.DataplaneSRRoute, error) {
	panic("unimplemented")
}

// GetSRRouteForPrefix implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) GetSRRouteForPrefix() ([]dataplane.DataplaneSRRoute, error) {
	panic("unimplemented")
}

// InsertSRRouteForPrefix implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) InsertSRRouteForPrefix(prefix netip.Prefix, tableID int, segments []netip.Addr, metric int) error {
	panic("unimplemented")
}

// RemoveSRRouteForPrefix implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) RemoveSRRouteForPrefix(prefix netip.Prefix, tableID int, metric int) error {
	panic("unimplemented")
}

// GetSIDs implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) GetSIDs(filterBy *dataplane.EncapType, groupingKey *int) ([]dataplane.DataplaneSID, error) {
	panic("unimplemented")
}

// RemoveSID implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) RemoveSID(sid netip.Addr, tableID int) error {
	panic("unimplemented")
}

// UpsertDT46SID implements [dataplane.Dataplane].
func (t *TestDefaultRouteOnlyDataplane) UpsertDT46SID(sid netip.Addr, vrfTableID int, tableID int) error {
	panic("unimplemented")
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

func (t *TestDefaultRouteOnlyDataplane) InsertPrefixRoute(tunnelIface string, tableID int, prefix netip.Prefix, via netip.Addr, metric int, allowLeak bool) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) InsertXFRMInterface(interfaceName string, underLayIface string, ifID uint32, masterVRFName *string, linkLocalAddr netip.Prefix) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) RemovePrefixRoute(prefix netip.Prefix, tableID int, metric int) error {
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

func (t *TestDefaultRouteOnlyDataplane) GetRules() ([]dataplane.DataplaneRule, error) {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) UpsertVRF(tableName string, tableID int) error {
	panic("unimplemented")
}

var _ dataplane.Dataplane = (*TestDefaultRouteOnlyDataplane)(nil)

func (t *TestDefaultRouteOnlyDataplane) InsertLimit(iifname string, mark *uint32, exceptMarks []uint32, rateBytes uint64, burstBytes uint32) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) GetLimits() ([]dataplane.DataplaneLimit, error) {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) RemoveLimit(handle uint64) error {
	panic("unimplemented")
}

func (t *TestDefaultRouteOnlyDataplane) InsertBFDPeer(profile string, addr netip.Addr, vrf string, iface string) error {
	return nil
}

func (t *TestDefaultRouteOnlyDataplane) DeleteBFDPeer(addr netip.Addr, vrf string, iface string) error {
	return nil
}

func (t *TestDefaultRouteOnlyDataplane) GetBFDPeers() ([]dataplane.DataplaneBFDPeer, error) {
	return nil, nil
}
