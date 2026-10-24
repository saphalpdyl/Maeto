package dataplane

import (
	"context"
	"fmt"
	"hash/fnv"
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
		Src:      &cpe.SitePrefix,
		Table:    CPETunnelTable,
	}
	resources[siteRule.ID().Key] = siteRule

	return resources, nil
}

// Bitwise structure of a Per-Color Routing table ID
// 0                  1                  2                  3
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |     Color     |                   VRFTableID                  |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
const (
	vrfTableBits = 24
	colorBits    = 8
)

// We need to make sure that StringToID produces a tableID
// that occupies the lower vrfTableBits bits.
func StringToID(s string) uint32 {
	const minVal uint64 = 300
	const span uint64 = (1 << vrfTableBits) - minVal

	h := fnv.New64a()
	h.Write([]byte(s))
	val64 := h.Sum64()

	return uint32(minVal + (val64 % span))
}

// Unlike other resources, routing tables in the linux kernel are not
// distinct kernel objects. They are just an ID that routes and rules
// point towards. So determinstic geneartion of the said routing table
// "creates" the table
func ColorTableID(vrfTable int, color uint64) int {
	return int(color)<<vrfTableBits | vrfTable
}

func (r *Reconciler) RenderPEIntent(
	ctx context.Context,
	pe *nodesync.PEIntent,
) (map[string]Resource, error) {
	routeTableCollisionMap := make(map[int]struct{})
	resources := make(map[string]Resource)

	for _, tIntent := range pe.TrafficClassIntent {
		for _, dscp := range tIntent.DSCP {
			if dscp >= 255 {
				return nil, fmt.Errorf("dscp (%d) is too big", dscp)
			}

			classification := &Classification{
				Classifier: uint32(dscp),
				Mark:       uint32(tIntent.Color),
			}

			resources[classification.ID().Key] = classification
		}
	}

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
		uniqueColors := make(map[uint64]struct{})
		for _, path := range t.InstallPaths {
			// Netlink installs the routes backwards for some reason
			// So reversing the routes during rendering
			reverseSegments := slices.Clone(path.Segments)
			slices.Reverse(reverseSegments)

			for _, prefix := range path.PrefixRoutes {
				routingTableID := ColorTableID(int(tableID), path.Color) // unique per-color per-tenant

				uniqueColors[path.Color] = struct{}{}

				if _, ok := routeTableCollisionMap[routingTableID]; ok {
					r.logger.ErrorContext(ctx, "found route table ID collision", "tenant", tenantID, "id", routingTableID)

					// Deny installing path if there's a collision
					continue
				}

				srRoute := &SRRoute{
					Prefix:   prefix,
					Segments: reverseSegments,
					Color:    path.Color,
					Table:    routingTableID,
				}

				resources[srRoute.ID().Key] = srRoute
				routeTableCollisionMap[routingTableID] = struct{}{}
			}

		}

		for color := range uniqueColors {
			rule := &Rule{
				Priority: 80,
				Table:    ColorTableID(int(tableID), color),
				Mark:     int(color),
				Iif:      vrfTableName,
			}

			resources[rule.ID().Key] = rule
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
	classifications []DataplaneClassification,
) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	for _, c := range classifications {
		classification := &Classification{
			Classifier: c.Classifier,
			Mark:       c.Mark,
		}

		resources[classification.ID().Key] = classification
	}

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
		rule := &Rule{
			Priority: r.Priority,
			Table:    r.Table,
			Mark:     r.Mark,
			Iif:      r.Iif,
		}
		if r.Src != nil {
			rule.Src = r.Src
		}

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
			Color:    uint64(r.Color),
			Table:    r.Table,
		}

		resources[srRoute.ID().Key] = srRoute
	}

	return resources, nil
}
