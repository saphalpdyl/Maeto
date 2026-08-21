package dataplane

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var _ Dataplane = (*LinuxNetlink)(nil)

// metric for the VRF catch-all, kept as a number rather than the shell string
const vrfUnreachableMetricValue = 4278198272

// every link and route maeto installs carries these, so a reconcile can list
// exactly what it owns and leave everything else alone
const (
	MaetoLinkGroup  uint32                = 211
	MaetoRouteProto netlink.RouteProtocol = 211
)

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

	routes := make([]DataplaneRoute, 0, len(netlinkRoutes))
	for _, r := range netlinkRoutes {
		route := DataplaneRoute{
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

		{
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
	}

	if err := netlink.LinkSetUp(vrf); err != nil {
		return fmt.Errorf("bring up vrf device %s: %w", tableName, err)
	}

	if err := netlink.RouteReplace(vrfCatchAll(tableId)); err != nil {
		return fmt.Errorf("install vrf catch-all for %s: %w", tableName, err)
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
	var maetoLinks []DataplaneLink

	for _, l := range links {
		if l.Attrs().Group == MaetoLinkGroup && l.Type() == ifaceType {
			maetoLinks = append(maetoLinks, DataplaneLink{
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
			})
		}
	}

	return maetoLinks, nil
}

// InsertPrefixRoute points prefix at tunnelIface in tableID. Pass
// unix.RT_TABLE_MAIN on a cpe, where there is no vrf to speak of.
func (l *LinuxNetlink) InsertPrefixRoute(tunnelIface string, tableID int, prefix netip.Prefix) error {
	iface, err := netlink.LinkByName(tunnelIface)
	if err != nil {
		return fmt.Errorf("lookup tunnel interface %s: %w", tunnelIface, err)
	}

	// a route in a vrf table pointing at an interface outside that vrf would
	// install cleanly and blackhole, so check enslavement when there is a vrf
	if tableID != unix.RT_TABLE_MAIN {
		vrf, err := vrfByTable(tableID)
		if err != nil {
			return err
		}

		if iface.Attrs().MasterIndex != vrf.Attrs().Index {
			return fmt.Errorf("%s is not enslaved to %s (table %d)",
				tunnelIface, vrf.Attrs().Name, tableID)
		}
	}

	route := &netlink.Route{
		LinkIndex: iface.Attrs().Index,
		Table:     tableID,
		Family:    netlink.FAMILY_V6,
		Protocol:  MaetoRouteProto,
		Dst:       prefixToIPNet(prefix),
	}

	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("insert prefix route %s @ %s: %w", prefix, tunnelIface, err)
	}

	return nil
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
func (l *LinuxNetlink) InsertXFRMInterface(interfaceName string, underLayIface string, ifID uint32, masterVRFIndex *int) error {
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

	if masterVRFIndex != nil {
		vrf, err := netlink.LinkByIndex(*masterVRFIndex)
		if err != nil {
			return fmt.Errorf("lookup vrf at index %d: %w", *masterVRFIndex, err)
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
