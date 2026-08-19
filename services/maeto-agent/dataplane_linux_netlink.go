package maetoagent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var _ Dataplane = (*LinuxNetlinkDataplane)(nil)

// metric for the VRF catch-all, kept as a number rather than the shell string
const vrfUnreachableMetricValue = 4278198272

const vrfStrictModePath = "/proc/sys/net/vrf/strict_mode"

type LinuxNetlinkDataplane struct{}

func NewLinuxNetlinkDataplane() *LinuxNetlinkDataplane {
	return &LinuxNetlinkDataplane{}
}

func (nl *LinuxNetlinkDataplane) AddVRF(_ context.Context, tableName string, tableId int) error {
	vrf := &netlink.Vrf{
		LinkAttrs: netlink.LinkAttrs{Name: tableName},
		Table:     uint32(tableId), // #nosec G115 -- table ids are small positive ints
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

	// this prevents route lookup from falling through when it doesn't match a
	// route. Weird Linux VRF implementation thing
	unreachable := &netlink.Route{
		Type:     unix.RTN_UNREACHABLE,
		Table:    tableId,
		Family:   netlink.FAMILY_V6,
		Dst:      &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)},
		Priority: vrfUnreachableMetricValue,
	}
	if err := netlink.RouteReplace(unreachable); err != nil {
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

func (nl *LinuxNetlinkDataplane) InsertXFRMInterface(_ context.Context, interfaceName string, underLayIface string, ifID uint32, vrfTableName string) error {
	underlay, err := netlink.LinkByName(underLayIface)
	if err != nil {
		return fmt.Errorf("lookup underlay %s: %w", underLayIface, err)
	}

	vrf, err := netlink.LinkByName(vrfTableName)
	if err != nil {
		return fmt.Errorf("lookup vrf %s: %w", vrfTableName, err)
	}

	xfrm := &netlink.Xfrmi{
		LinkAttrs: netlink.LinkAttrs{
			Name:        interfaceName,
			ParentIndex: underlay.Attrs().Index,
		},
		Ifid: ifID,
	}

	if err := netlink.LinkAdd(xfrm); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("create xfrm device %s: %w", interfaceName, err)
		}

		existing, err := netlink.LinkByName(interfaceName)
		if err != nil {
			// EEXIST without the name present means another link already holds
			// this if_id -- the kernel enforces if_id uniqueness
			return fmt.Errorf("if_id %d is already bound by another interface: %w", ifID, err)
		}

		current, ok := existing.(*netlink.Xfrmi)
		if !ok {
			return fmt.Errorf("%s exists but is %T, not an xfrm interface", interfaceName, existing)
		}

		// if_id is immutable on a live link, so a stale one has to be replaced
		if current.Ifid != ifID {
			if err := netlink.LinkDel(current); err != nil {
				return fmt.Errorf("delete stale xfrm %s (if_id %d): %w", interfaceName, current.Ifid, err)
			}
			if err := netlink.LinkAdd(xfrm); err != nil {
				return fmt.Errorf("recreate xfrm device %s: %w", interfaceName, err)
			}
		} else {
			xfrm = current
		}
	}

	if err := netlink.LinkSetMaster(xfrm, vrf); err != nil {
		return fmt.Errorf("enslave %s to %s: %w", interfaceName, vrfTableName, err)
	}

	if err := netlink.LinkSetUp(xfrm); err != nil {
		return fmt.Errorf("bring up xfrm device %s: %w", interfaceName, err)
	}

	return nil
}

func (nl *LinuxNetlinkDataplane) InsertReturnPrefix(_ context.Context, tunnelIface string, vrfTableName string, prefix netip.Prefix) error {
	vrfLink, err := netlink.LinkByName(vrfTableName)
	if err != nil {
		return fmt.Errorf("lookup vrf %s: %w", vrfTableName, err)
	}

	vrf, ok := vrfLink.(*netlink.Vrf)
	if !ok {
		return fmt.Errorf("%s is not a vrf device (%T)", vrfTableName, vrfLink)
	}

	iface, err := netlink.LinkByName(tunnelIface)
	if err != nil {
		return fmt.Errorf("lookup tunnel interface %s: %w", tunnelIface, err)
	}

	// a route in the vrf table pointing at an interface outside the vrf would
	// install cleanly and blackhole
	if iface.Attrs().MasterIndex != vrf.Attrs().Index {
		return fmt.Errorf("%s is not enslaved to %s", tunnelIface, vrfTableName)
	}

	route := &netlink.Route{
		LinkIndex: iface.Attrs().Index,
		Table:     int(vrf.Table), // #nosec G115 -- table ids are small positive ints
		Family:    netlink.FAMILY_V6,
		Dst: &net.IPNet{
			IP:   net.IP(prefix.Masked().Addr().AsSlice()),
			Mask: net.CIDRMask(prefix.Bits(), 128),
		},
	}

	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("insert return prefix %s into vrf %s @ %s: %w", prefix, vrfTableName, tunnelIface, err)
	}

	return nil
}

// nexthop objects (RTM_NEWNEXTHOP) have no binding in vishvananda/netlink, so
// these two stay on iproute2. `replace` makes both idempotent already.
func (nl *LinuxNetlinkDataplane) UpsertPolicy(ctx context.Context, nhid, dtsid, vrfTableId, vrfTableName string, sids []string) error {
	segs := append(append([]string{}, sids...), dtsid)

	if err := run(ctx, "ip", "-6", "nexthop", "replace", "id", nhid,
		"encap", "seg6", "mode", "encap",
		"segs", strings.Join(segs, ","),
		"dev", "sr0"); err != nil {
		return fmt.Errorf("upsert seg6 nexthop id %s: %w", nhid, err)
	}

	return nil
}

func (nl *LinuxNetlinkDataplane) UpsertRouteToPolicy(ctx context.Context, dest, vrfTableId, nhid string) error {
	if err := run(ctx, "ip", "-6", "route", "replace", dest,
		"table", vrfTableId, "nhid", nhid); err != nil {
		return fmt.Errorf("upsert route %s -> nhid %s in table %s: %w", dest, nhid, vrfTableId, err)
	}

	return nil
}

func (nl *LinuxNetlinkDataplane) GetDefaultRouteAndDev(_ context.Context) (string, string, error) {
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
