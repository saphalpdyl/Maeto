package dataplane

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"net/netip"
	"slices"

	"github.com/saphalpdyl/maeto/libs/nodesync"
)

// Rendering is the process of converting high-level intent or low-level dataplane state
// into domain-specific canonical structure so the two can be diffed directly.

// RenderCPEIntent builds the cpe side: one tunnel interface, a default route
// into it in maeto's own table, and a rule sending this site's traffic there.
func (r *Reconciler) RenderCPEIntent(ctx context.Context, cpe *nodesync.CPEIntent) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	if cpe.TunnelInterfaceID == 0 {
		r.logger.WarnContext(ctx, "tunnel iface id == 0: intent is not ready to be installed")
		return resources, nil
	}

	if !cpe.SitePrefix.IsValid() {
		return nil, fmt.Errorf("cpe intent has no site prefix, nothing could select the tunnel")
	}

	xfrmTunnelName := fmt.Sprintf("xfrm-tenant-%s", cpe.TenantID)
	xfrm := &XFRM{
		Name:      xfrmTunnelName,
		IfID:      cpe.TunnelInterfaceID,
		Parent:    "lo",
		MasterVRF: "", // no VRF for CPE
		Index:     0,  // observed kernel-assigned value
	}
	resources[xfrm.ID().Key] = xfrm

	tunnelDefault := &Route{
		Table:  CPETunnelTable,
		Dst:    netip.PrefixFrom(netip.IPv6Unspecified(), 0), // ::/0
		Dev:    xfrmTunnelName,
		Via:    netip.Addr{}, // device route: an xfrm interface is NOARP
		Metric: 0,
	}
	resources[tunnelDefault.ID().Key] = tunnelDefault

	siteRule := &Rule{
		Priority: CPERulePriority,
		Src:      cpe.SitePrefix,
		Table:    CPETunnelTable,
	}
	resources[siteRule.ID().Key] = siteRule

	return resources, nil
}

func StringToID(s string) uint32 {
	const minVal uint64 = 300
	const span uint64 = math.MaxUint32 - minVal + 1

	h := fnv.New64a()
	h.Write([]byte(s))
	val64 := h.Sum64()

	return uint32(minVal + (val64 % span))
}

func (r *Reconciler) RenderPEIntent(
	ctx context.Context,
	pe *nodesync.PEIntent,
) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	for tenantID, t := range pe.Tenants {
		vrfTableName := fmt.Sprintf("maeto-vrf-%s", tenantID)
		tableID := StringToID(tenantID)

		vrf := &VRF{
			Name:    vrfTableName,
			TableID: tableID,
			Index:   0,
		}

		resources[vrf.ID().Key] = vrf

		if t.DT46SID.IsValid() {
			dt46SID := &SID{
				SIDType: SIDDT46,
				SID:     t.DT46SID,
				TableID: int(tableID),
				Metric:  0,
			}

			resources[dt46SID.ID().Key] = dt46SID
		}

		for _, portalIntent := range t.PortalIntents {
			xfrmName := fmt.Sprintf("maeto-tun-%s-%d", tenantID, portalIntent.TunnelInterfaceID)

			xfrm := &XFRM{
				Name:      xfrmName,
				IfID:      portalIntent.TunnelInterfaceID,
				Parent:    portalIntent.HostFacingInterface,
				MasterVRF: vrfTableName,
				Index:     0,
			}

			resources[xfrm.ID().Key] = xfrm

			route := &Route{
				Table:  int(tableID),
				Dst:    portalIntent.SitePrefix,
				Dev:    xfrmName,
				Via:    netip.Addr{},
				Metric: 0,
			}

			resources[route.ID().Key] = route
		}

		// SRv6 Routes
		for _, path := range t.InstallPaths {
			// Netlink installs the routes backwards for some reason
			// So reversing the routes during rendering
			reverseSegments := slices.Clone(path.Segments)
			slices.Reverse(reverseSegments)

			for _, prefix := range path.PrefixRoutes {
				srRoute := &SRRoute{
					Prefix:   prefix,
					Segments: reverseSegments,
					Color:    0,
					Table:    int(tableID),
				}

				resources[srRoute.ID().Key] = srRoute
			}
		}
	}

	return resources, nil
}

func (r *Reconciler) RenderFIB(
	ctx context.Context,
	vrfLinks []*DataplaneVRF,
	xfrmLinks []*DataplaneXFRM,
	routes []DataplaneRoute,
	sids []DataplaneSID,
	rules []DataplaneRule,
	srRoutes []DataplaneSRRoute,
) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	for _, l := range vrfLinks {
		vrf := &VRF{
			Name:    l.Name,
			TableID: uint32(l.TableID),
			Index:   l.Index,
		}

		resources[vrf.ID().Key] = vrf
	}

	for _, l := range xfrmLinks {
		xfrm := &XFRM{
			Name:      l.Name,
			IfID:      l.IfID,
			Parent:    l.ParentName,
			MasterVRF: l.MasterName,
			Index:     l.Index,
		}

		resources[xfrm.ID().Key] = xfrm
	}

	for _, r := range routes {
		ip, _ := netip.AddrFromSlice(r.Dst.IP)
		ones, _ := r.Dst.Mask.Size()
		viaip, _ := netip.AddrFromSlice(r.Via.Addr)

		route := &Route{
			Table:  r.Table,
			Dst:    netip.PrefixFrom(ip, ones),
			Dev:    r.Dev,
			Via:    viaip,
			Metric: 0,
		}

		resources[route.ID().Key] = route
	}

	for _, r := range rules {
		rule := &Rule{Priority: r.Priority, Src: r.Src, Table: r.Table}
		resources[rule.ID().Key] = rule
	}

	for _, s := range sids {
		ip, _ := netip.AddrFromSlice(s.Dst.IP)

		sid := &SID{
			SIDType: SIDType(s.EncapType),
			SID:     ip,
			TableID: s.VrfTableID,
			Metric:  0,
		}

		resources[sid.ID().Key] = sid
	}

	for _, r := range srRoutes {
		ip, _ := netip.AddrFromSlice(r.Dst.IP)
		ones, _ := r.Dst.Mask.Size()

		srRoute := &SRRoute{
			Prefix:   netip.PrefixFrom(ip, ones),
			Segments: r.Segments,
			Color:    0,
			Table:    r.Table,
		}

		resources[srRoute.ID().Key] = srRoute
	}

	return resources, nil
}
