package dataplane

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
	nl "github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

var _ Dataplane = (*LinuxNetlink)(nil)

// metric for the VRF catch-all, kept as a number rather than the shell string
const vrfUnreachableMetricValue = 4278198272
const vrfStrictModePath = "/proc/sys/net/vrf/strict_mode"

// every link and route maeto installs carries these, so a reconcile can list
// exactly what it owns and leave everything else alone
const (
	MaetoLinkGroup  uint32                = 211
	MaetoRouteProto netlink.RouteProtocol = 211
	// SIDs are basically Routes so we don't want them to get fetch during route fetch
	MaetoSIDProto netlink.RouteProtocol = 212
	// For encap seg6 routes; MaetoSIDProto is for seg6local routes
	MaetoSIDSegsProto netlink.RouteProtocol = 213
)

// SEG6_LOCAL_ACTION_END_DT46 from uapi/linux/seg6_local.h. vishvananda/netlink
// v1.3.1 stops its action enum at END_BPF, so this one is spelled out.
const seg6LocalActionEndDT46 = 16

type LinuxNetlink struct{}

// RemoveVRF deletes the vrf device at linkIndex. IFLA_GROUP is not a match key
// for RTM_DELLINK, so ownership has to be checked before deleting, unlike
// routes where the kernel enforces it via the protocol field.
func (l *LinuxNetlink) RemoveVRF(linkIndex int) error {
	link, err := netlink.LinkByIndex(linkIndex)
	if err != nil {
		if errors.Is(err, unix.ENODEV) || isLinkNotFound(err) {
			return nil // already gone
		}
		return fmt.Errorf("lookup vrf at index %d: %w", linkIndex, err)
	}

	vrf, ok := link.(*netlink.Vrf)
	if !ok {
		return fmt.Errorf("link at index %d is %T, not a vrf", linkIndex, link)
	}

	if link.Attrs().Group != MaetoLinkGroup {
		return fmt.Errorf("vrf %s at index %d is not maeto owned (group %d)",
			link.Attrs().Name, linkIndex, link.Attrs().Group)
	}

	// the catch-all sits on lo rather than the vrf device, so deleting the
	// device would strand it in the table
	if err := netlink.RouteDel(vrfCatchAll(int(vrf.Table))); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("remove vrf catch-all for table %d: %w", vrf.Table, err)
	}

	if err := netlink.LinkDel(link); err != nil {
		if errors.Is(err, unix.ENODEV) {
			return nil
		}
		return fmt.Errorf("delete vrf device at index %d: %w", linkIndex, err)
	}

	return nil
}

func (l *LinuxNetlink) RemoveXFRMInterface(linkIndex int) error {
	link, err := netlink.LinkByIndex(linkIndex)
	if err != nil {
		if errors.Is(err, unix.ENODEV) || isLinkNotFound(err) {
			return nil // already gone
		}
		return fmt.Errorf("lookup xfrm interface at index %d: %w", linkIndex, err)
	}

	if _, ok := link.(*netlink.Xfrmi); !ok {
		return fmt.Errorf("link at index %d is %T, not an xfrm interface", linkIndex, link)
	}

	if link.Attrs().Group != MaetoLinkGroup {
		return fmt.Errorf("xfrm interface %s at index %d is not maeto owned (group %d)",
			link.Attrs().Name, linkIndex, link.Attrs().Group)
	}

	if err := netlink.LinkDel(link); err != nil {
		if errors.Is(err, unix.ENODEV) {
			return nil
		}
		return fmt.Errorf("delete xfrm interface at index %d: %w", linkIndex, err)
	}

	return nil
}

// netlink reports a missing link as its own error type rather than ENODEV on
// the lookup path
func isLinkNotFound(err error) bool {
	var notFound netlink.LinkNotFoundError
	return errors.As(err, &notFound)
}

// RemovePrefixRoute deletes prefix from tableID. Protocol is a match key for
// RTM_DELROUTE, so this can only ever remove a route maeto installed.
func (l *LinuxNetlink) RemovePrefixRoute(prefix netip.Prefix, tableID int) error {
	err := netlink.RouteDel(&netlink.Route{
		Dst:      prefixToIPNet(prefix),
		Table:    tableID,
		Family:   netlink.FAMILY_V6,
		Protocol: MaetoRouteProto,
	})
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			return nil // already gone
		}
		return fmt.Errorf("delete prefix route %s from table %d: %w", prefix, tableID, err)
	}

	return nil
}

func prefixToIPNet(prefix netip.Prefix) *net.IPNet {
	return &net.IPNet{
		IP:   net.IP(prefix.Masked().Addr().AsSlice()),
		Mask: net.CIDRMask(prefix.Bits(), prefix.Addr().BitLen()),
	}
}

// GetPrefixRoutes lists every route maeto owns, across all tables. RouteList
// returns the main table only, so the table filter has to be set explicitly
// even though the value is "unspecified".
func (l *LinuxNetlink) GetPrefixRoutes() ([]DataplaneRoute, error) {
	netlinkRoutes, err := netlink.RouteListFiltered(netlink.FAMILY_ALL,
		&netlink.Route{Table: unix.RT_TABLE_UNSPEC, Protocol: MaetoRouteProto},
		netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		return nil, fmt.Errorf("list maeto routes: %w", err)
	}

	names, err := linkNames()
	if err != nil {
		return nil, err
	}

	routes := make([]DataplaneRoute, 0, len(netlinkRoutes))
	for _, r := range netlinkRoutes {
		route := DataplaneRoute{
			Dev:        names[r.LinkIndex],
			LinkIndex:  r.LinkIndex,
			ILinkIndex: r.ILinkIndex,
			Dst:        r.Dst,
			Family:     r.Family,
			Table:      r.Table,
			Type:       r.Type,
			Tos:        r.Tos,
			Realm:      r.Realm,
			MTU:        r.MTU,
			Rtt:        r.Rtt,
			AdvMSS:     r.AdvMSS,
		}

		// Via is nil on an ordinary route, and only *netlink.Via carries an address
		if via, ok := r.Via.(*netlink.Via); ok && via != nil {
			route.Via = Via{AddrFamily: via.AddrFamily, Addr: via.Addr}
		}

		routes = append(routes, route)
	}

	return routes, nil
}

func NewLinuxNetlink() *LinuxNetlink {
	return &LinuxNetlink{}
}

func (l *LinuxNetlink) UpsertVRF(tableName string, tableId int) error {
	vrf := &netlink.Vrf{
		LinkAttrs: netlink.LinkAttrs{
			Group: MaetoLinkGroup,
			Name:  tableName,
		},
		Table: uint32(tableId), // #nosec G115 -- table ids are small positive ints
	}

	if err := netlink.LinkAdd(vrf); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("create vrf device %s: %w", tableName, err)
		}

		existing, err := netlink.LinkByName(tableName)
		if err != nil {
			return fmt.Errorf("lookup existing vrf %s: %w", tableName, err)
		}

		current, ok := existing.(*netlink.Vrf)
		if !ok {
			return fmt.Errorf("%s exists but is %T, not a vrf", tableName, existing)
		}

		// a table change would silently strand every route already in the old
		// table, so refuse rather than converge
		if current.Table != vrf.Table {
			return fmt.Errorf("vrf %s exists with table %d, wanted %d", tableName, current.Table, vrf.Table)
		}

		vrf = current
	}

	if err := netlink.LinkSetUp(vrf); err != nil {
		return fmt.Errorf("bring up vrf device %s: %w", tableName, err)
	}

	if err := netlink.RouteReplace(vrfCatchAll(tableId)); err != nil {
		return fmt.Errorf("install vrf catch-all for %s: %w", tableName, err)
	}

	// https://onvox.net/2024/12/16/srv6-frr/#usid-caveat
	// It is important to note that [net.vrf.strict_mode=1] setting gets reset any
	// 	time a new VRF is added to the kernel and must be reset again
	if err := os.WriteFile(vrfStrictModePath, []byte("1"), 0o644); err != nil { // #nosec G306 -- this is a sysctl, not a user file
		return fmt.Errorf("enable vrf strict mode: %w", err)
	}

	return nil
}

// vrfCatchAll stops a lookup in the vrf table from falling through to the
// tables after it. Weird Linux VRF implementation thing.
func vrfCatchAll(tableID int) *netlink.Route {
	return &netlink.Route{
		Type:     unix.RTN_UNREACHABLE,
		Table:    tableID,
		Family:   netlink.FAMILY_V6,
		Priority: vrfUnreachableMetricValue,
		Protocol: MaetoRouteProto,
		Dst: &net.IPNet{
			IP:   net.IPv6zero,
			Mask: net.CIDRMask(0, 128),
		},
	}
}

// linkNames maps every interface index to its name, so read methods can hand
// back names instead of kernel indexes
func linkNames() (map[int]string, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}

	out := make(map[int]string, len(links))
	for _, link := range links {
		out[link.Attrs().Index] = link.Attrs().Name
	}

	return out, nil
}

func maetoRule(priority int, src netip.Prefix, tableID int) *netlink.Rule {
	rule := netlink.NewRule()
	rule.Family = netlink.FAMILY_V6
	rule.Priority = priority
	rule.Src = prefixToIPNet(src)
	rule.Table = tableID
	rule.Protocol = uint8(MaetoRouteProto)

	return rule
}

// UpsertRule installs a policy rule. RuleAdd is not idempotent -- a second
// identical add returns EEXIST rather than succeeding.
func (l *LinuxNetlink) UpsertRule(priority int, src netip.Prefix, tableID int) error {
	if err := netlink.RuleAdd(maetoRule(priority, src, tableID)); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return nil // already there, and every field is part of its identity
		}

		return fmt.Errorf("add rule from %s lookup %d: %w", src, tableID, err)
	}

	return nil
}

// RemoveRule deletes a policy rule. Unlike routes, a missing rule reports
// ENOENT rather than ESRCH.
func (l *LinuxNetlink) RemoveRule(priority int, src netip.Prefix, tableID int) error {
	if err := netlink.RuleDel(maetoRule(priority, src, tableID)); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil // already gone
		}

		return fmt.Errorf("delete rule from %s lookup %d: %w", src, tableID, err)
	}

	return nil
}

// GetRules lists the rules maeto owns. The kernel's own rules carry
// RTPROT_KERNEL, so the protocol tag separates ours from local/main/l3mdev.
func (l *LinuxNetlink) GetRules() ([]DataplaneRule, error) {
	rules, err := netlink.RuleList(netlink.FAMILY_V6)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}

	out := make([]DataplaneRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Protocol != uint8(MaetoRouteProto) || rule.Src == nil {
			continue
		}

		src, ok := netip.AddrFromSlice(rule.Src.IP)
		if !ok {
			continue
		}
		ones, _ := rule.Src.Mask.Size()

		out = append(out, DataplaneRule{
			Priority: rule.Priority,
			Src:      netip.PrefixFrom(src, ones),
			Table:    rule.Table,
		})
	}

	return out, nil
}

func (l *LinuxNetlink) GetDefaultRouteAndDev() (string, string, error) {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6,
		&netlink.Route{Dst: nil}, netlink.RT_FILTER_DST)
	if err != nil {
		return "", "", fmt.Errorf("list ipv6 default routes: %w", err)
	}

	// lowest metric wins, matching the kernel's own selection
	best := -1
	for i, r := range routes {
		if r.Gw == nil || r.LinkIndex == 0 {
			continue
		}
		if best == -1 || r.Priority < routes[best].Priority {
			best = i
		}
	}

	if best == -1 {
		return "", "", fmt.Errorf("no ipv6 default route with a gateway")
	}

	link, err := netlink.LinkByIndex(routes[best].LinkIndex)
	if err != nil {
		return "", "", fmt.Errorf("resolve link index %d: %w", routes[best].LinkIndex, err)
	}

	return routes[best].Gw.String(), link.Attrs().Name, nil
}

func (l *LinuxNetlink) GetLinksByType(ifaceType string) ([]DataplaneLink, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}

	names := make(map[int]string, len(links))
	for _, link := range links {
		names[link.Attrs().Index] = link.Attrs().Name
	}
	var maetoLinks []DataplaneLink

	for _, l := range links {
		if l.Attrs().Group == MaetoLinkGroup && l.Type() == ifaceType {
			dpLinkAttrs := DataplaneLinkAttrs{
				Index:        l.Attrs().Index,
				MTU:          l.Attrs().MTU,
				TxQLen:       l.Attrs().TxQLen,
				Name:         l.Attrs().Name,
				HardwareAddr: l.Attrs().HardwareAddr,
				Flags:        l.Attrs().Flags,
				RawFlags:     l.Attrs().RawFlags,
				Alias:        l.Attrs().Alias,
				EncapType:    l.Attrs().EncapType,
				PhysSwitchID: l.Attrs().PhysSwitchID,
				NumTxQueues:  l.Attrs().NumTxQueues,
				NumRxQueues:  l.Attrs().NumRxQueues,
				Group:        l.Attrs().Group,
				PermHWAddr:   l.Attrs().PermHWAddr,
				ParentIndex:  l.Attrs().ParentIndex,
				MasterIndex:  l.Attrs().MasterIndex,
				ParentName:   names[l.Attrs().ParentIndex],
				MasterName:   names[l.Attrs().MasterIndex],
			}

			if l.Type() == "xfrm" {
				l, ok := l.(*netlink.Xfrmi)
				if !ok {
					return nil, fmt.Errorf("got type == xfrm, couldn't parse to Xfrmi")
				}

				maetoLinks = append(maetoLinks, &DataplaneXFRM{
					DataplaneLinkAttrs: dpLinkAttrs,
					IfID:               l.Ifid,
				})

				continue
			}

			if l.Type() == "vrf" {
				vrf, ok := l.(*netlink.Vrf)
				if !ok {
					return nil, fmt.Errorf("got type == vrf, couldn't parse to Vrf")
				}

				maetoLinks = append(maetoLinks, &DataplaneVRF{
					DataplaneLinkAttrs: dpLinkAttrs,
					TableID:            vrf.Table,
				})

				continue
			}
		}
	}

	return maetoLinks, nil
}

// InsertPrefixRoute installs prefix in tableID. A valid via makes it a gateway
// route and the kernel resolves the outgoing device itself -- naming a device
// the gateway is not on-link for is rejected outright. An invalid via makes it
// a device route out tunnelIface, which is what a NOARP xfrm interface needs.
// tableID unix.RT_TABLE_MAIN (or 0) is the main table, the cpe case.
func (l *LinuxNetlink) InsertPrefixRoute(tunnelIface string, tableID int, prefix netip.Prefix, via netip.Addr) error {
	route := &netlink.Route{
		Table:    tableID,
		Family:   netlink.FAMILY_V6,
		Protocol: MaetoRouteProto,
		Dst:      prefixToIPNet(prefix),
	}

	if via.IsValid() {
		// LinkIndex stays unset: the kernel picks the device by looking up the
		// gateway, which is more robust than pinning one that may change
		route.Gw = net.IP(via.AsSlice())
	} else {
		iface, err := netlink.LinkByName(tunnelIface)
		if err != nil {
			return fmt.Errorf("lookup tunnel interface %s: %w", tunnelIface, err)
		}

		// a route in a vrf table pointing at an interface outside that vrf
		// would install cleanly and blackhole, so check enslavement -- but only
		// when a vrf actually owns the table. A cpe's tunnel table is a plain
		// policy routing table with no vrf behind it.
		if vrf := vrfByTableOrNil(tableID); vrf != nil {
			if iface.Attrs().MasterIndex != vrf.Attrs().Index {
				return fmt.Errorf("%s is not enslaved to %s (table %d)",
					tunnelIface, vrf.Attrs().Name, tableID)
			}
		}

		route.LinkIndex = iface.Attrs().Index
	}

	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("insert prefix route %s (via %v, dev %s, table %d): %w",
			prefix, via, tunnelIface, tableID, err)
	}

	return nil
}

// vrfByTableOrNil returns the vrf bound to tableID, or nil when the table is
// not a vrf table at all
func vrfByTableOrNil(tableID int) *netlink.Vrf {
	if tableID == unix.RT_TABLE_MAIN || tableID == 0 {
		return nil
	}

	vrf, err := vrfByTable(tableID)
	if err != nil {
		return nil
	}

	return vrf
}

func vrfByTable(tableID int) (*netlink.Vrf, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}

	for _, link := range links {
		if vrf, ok := link.(*netlink.Vrf); ok && int(vrf.Table) == tableID {
			return vrf, nil
		}
	}

	return nil, fmt.Errorf("no vrf device owns table %d", tableID)
}

// InsertXFRMInterface implements [Dataplane].
// masterVRFIndex == nil means no vrf master, which is the cpe case
func (l *LinuxNetlink) InsertXFRMInterface(interfaceName string, underLayIface string, ifID uint32, masterVRFName *string) error {
	underlay, err := netlink.LinkByName(underLayIface)
	if err != nil {
		return fmt.Errorf("lookup underlay interface %s: %w", underLayIface, err)
	}

	xfrm := &netlink.Xfrmi{
		LinkAttrs: netlink.LinkAttrs{
			Name:        interfaceName,
			ParentIndex: underlay.Attrs().Index,
			Group:       MaetoLinkGroup,
		},
		Ifid: ifID,
	}

	if err := netlink.LinkAdd(xfrm); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("create xfrm device %s: %w", interfaceName, err)
		}

		return fmt.Errorf("xfrm device %s already exists", interfaceName)
	}

	if masterVRFName != nil {
		vrf, err := netlink.LinkByName(*masterVRFName)
		if err != nil {
			return fmt.Errorf("lookup vrf with name %s: %w", *masterVRFName, err)
		}

		if err := netlink.LinkSetMaster(xfrm, vrf); err != nil {
			return fmt.Errorf("enslave %s to %s: %w", interfaceName, vrf.Attrs().Name, err)
		}
	}

	if err := netlink.LinkSetUp(xfrm); err != nil {
		return fmt.Errorf("bring up xfrm device %s: %w", interfaceName, err)
	}

	return nil
}

// UpsertPolicy implements [Dataplane].
func (l *LinuxNetlink) UpsertPolicy(nhid string, dtsid string, vrfTableId string, vrfTableName string, sids []string) error {
	segs := append(append([]string{}, sids...), dtsid)

	if err := run("ip", "-6", "nexthop", "replace", "id", nhid,
		"encap", "seg6", "mode", "encap",
		"segs", strings.Join(segs, ","),
		"dev", "sr0"); err != nil {
		return fmt.Errorf("upsert seg6 nexthop id %s: %w", nhid, err)
	}

	return nil
}

// UpsertRouteToPolicy implements [Dataplane].
func (l *LinuxNetlink) UpsertRouteToPolicy(dest string, vrfTableId string, nhid string) error {
	if err := run("ip", "-6", "route", "replace", dest,
		"table", vrfTableId, "nhid", nhid); err != nil {
		return fmt.Errorf("upsert route %s -> nhid %s in table %s: %w", dest, nhid, vrfTableId, err)
	}

	return nil
}

func (l *LinuxNetlink) UpsertDT46SID(sid netip.Addr, vrfTableID int) error {
	vrf, err := vrfByTable(vrfTableID)
	if err != nil {
		return fmt.Errorf("lookup vrf by table %d: %w", vrfTableID, err)
	}

	// End.DT46 accepts only the vrftable attribute -- the kernel rejects the
	// route outright if SEG6_LOCAL_TABLE is set alongside it
	encap := &netlink.SEG6LocalEncap{
		Action:   seg6LocalActionEndDT46,
		VrfTable: vrfTableID,
	}
	encap.Flags[nl.SEG6_LOCAL_VRFTABLE] = true

	route := &netlink.Route{
		Dst: &net.IPNet{
			IP:   net.IP(sid.AsSlice()),
			Mask: net.CIDRMask(128, 128),
		},
		Table:     unix.RT_TABLE_MAIN,
		Family:    netlink.FAMILY_V6,
		Protocol:  MaetoSIDProto,
		LinkIndex: vrf.Attrs().Index,
		Encap:     encap,
	}

	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("upsert DT46 sid %s (vrftable %d, dev %s): %w",
			sid, vrfTableID, vrf.Attrs().Name, err)
	}

	return nil
}

func (l *LinuxNetlink) RemoveSID(sid netip.Addr) error {
	// convert netip.Addr to net.IPNet
	parsedIPNet := &net.IPNet{
		IP:   net.IP(sid.AsSlice()),
		Mask: net.CIDRMask(128, 128),
	}

	err := netlink.RouteDel(&netlink.Route{
		Dst:      parsedIPNet,
		Table:    unix.RT_TABLE_MAIN,
		Family:   netlink.FAMILY_V6,
		Protocol: MaetoSIDProto,
	})

	if err != nil {
		return fmt.Errorf("remove sid %s: %w", sid, err)
	}

	return nil
}

func (l *LinuxNetlink) GetSIDs() ([]DataplaneSID, error) {
	netlinkRoutes, err := netlink.RouteListFiltered(netlink.FAMILY_ALL,
		&netlink.Route{Table: unix.RT_TABLE_UNSPEC, Protocol: MaetoSIDProto},
		netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		return nil, fmt.Errorf("list maeto sids: %w", err)
	}

	names, err := linkNames()
	if err != nil {
		return nil, err
	}

	sids := make([]DataplaneSID, 0, len(netlinkRoutes))
	for _, r := range netlinkRoutes {
		encap, ok := r.Encap.(*netlink.SEG6LocalEncap)
		if !ok {
			return nil, fmt.Errorf("failed to parse sid encap type as seg6localencap")
		}

		var encapType EncapType
		switch encap.Action {
		case nl.SEG6_LOCAL_ACTION_END_DT4:
			encapType = EncapTypeDT4
		case nl.SEG6_LOCAL_ACTION_END_DT6:
			encapType = EncapTypeDT6
		case seg6LocalActionEndDT46:
			encapType = EncapTypeDT46
		case nl.SEG6_LOCAL_ACTION_END_B6:
			encapType = EncapTypeB6
		default:
			return nil, fmt.Errorf("unknown encap action type %d", encap.Action)
		}

		sid := DataplaneSID{
			Dev:        names[r.LinkIndex],
			Dst:        r.Dst,
			Family:     r.Family,
			Table:      r.Table,
			Type:       r.Type,
			EncapType:  encapType,
			VrfTableID: encap.VrfTable,
		}

		sids = append(sids, sid)
	}

	return sids, nil
}
