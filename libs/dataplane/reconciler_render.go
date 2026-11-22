package dataplane

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/netip"
	"slices"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"golang.org/x/sys/unix"
)

// RENDERING
// Rendering is the process of converting high-level intent or low-level dataplane state
// into domain-specific canonical structure so the two can be diffed directly.

// RenderCPEIntent builds the cpe side: one tunnel interface, a default route
// into it in maeto's own table, and a rule sending this site's traffic there.
func (r *Reconciler) RenderCPEIntent(ctx context.Context, cpe *nodesync.CPEIntent) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	if len(cpe.Tunnels) == 0 {
		// No tunnels
		r.logger.WarnContext(ctx, "len(tunnels) == 0: intent is not ready to be installed")
		return resources, nil
	}

	if !cpe.SitePrefix.IsValid() {
		return nil, fmt.Errorf("cpe intent has no site prefix, nothing could select the tunnel")
	}

	xfrmPrefix, err := netip.ParsePrefix("fe80::2/64")
	if err != nil {
		return nil, fmt.Errorf("failed to parse xfrm prefix: %w", err)
	}

	for _, tunnel := range cpe.Tunnels {
		xfrmTunnelName := fmt.Sprintf("mto-tun-%s-%d", cpe.TenantID, tunnel.TunnelInterfaceID)
		xfrm := &XFRM{
			Name:          xfrmTunnelName,
			IfID:          tunnel.TunnelInterfaceID,
			Parent:        "lo",
			MasterVRF:     "", // no VRF for CPE
			LinkLocalAddr: xfrmPrefix,
			Index:         0, // observed kernel-assigned value
		}
		resources[xfrm.ID().Key] = xfrm

		bfdPeer := &BFDPeer{
			Profile:   TunnelBFDProfile,
			VRF:       "",
			Interface: xfrmTunnelName,
			Peer:      PETunnelLinkLocalAddr,
		}
		resources[bfdPeer.ID().Key] = bfdPeer

		tableID := StringToID(xfrmTunnelName)

		tunnelDefault := &Route{
			Table:  int(tableID),
			Dst:    netip.PrefixFrom(netip.IPv6Unspecified(), 0), // ::/0
			Dev:    xfrmTunnelName,
			Via:    netip.Addr{}, // device route: an xfrm interface is NOARP
			Metric: 0,
		}
		resources[tunnelDefault.ID().Key] = tunnelDefault

		if tunnel.Role == nodesync.TunnelRolePrimary {
			siteRule := &Rule{
				Priority: CPERulePriority,
				Src:      &cpe.SitePrefix,
				Table:    int(tableID),
			}

			resources[siteRule.ID().Key] = siteRule
		}
	}

	if trigger := cpeFailoverTrigger(cpe); trigger != nil {
		resources[trigger.ID().Key] = trigger
	}

	// Traffic from CPE addressed to its LAN (the site) was being bounced back and forth
	// in the tunnel because the rule above kept sending LAN traffic towards the tunnel
	// and the pop sent back the packet towards the CPE
	siteDstRule := &Rule{
		Priority: CPESiteRulePriority,
		Dst:      &cpe.SitePrefix,
		Table:    unix.RT_TABLE_MAIN,
	}
	resources[siteDstRule.ID().Key] = siteDstRule

	return resources, nil
}

func cpeFailoverTrigger(cpe *nodesync.CPEIntent) *RuleTrigger {
	var primary *nodesync.CPETunnelIntent
	var standby *nodesync.CPETunnelIntent

	for i := range cpe.Tunnels {
		tunnel := &cpe.Tunnels[i]

		if tunnel.Role == nodesync.TunnelRolePrimary {
			primary = tunnel
			continue
		}

		if standby == nil || tunnel.TunnelPE < standby.TunnelPE {
			standby = tunnel
		}
	}

	if primary == nil || standby == nil {
		return nil
	}

	standbyTunnelName := fmt.Sprintf("mto-tun-%s-%d", cpe.TenantID, standby.TunnelInterfaceID)

	return &RuleTrigger{
		Egress: primary.TunnelPE, // ActivateRepair sends the egress name of the node that failed
		Rules: []TriggerRule{
			{
				Src:      cpe.SitePrefix,
				Table:    int(StringToID(standbyTunnelName)),
				Priority: CPEFailoverRulePriority,
			},
		},
	}
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

// Both ends of every tunnel use the same pair; link-locals are scoped to the
// interface, so they never collide across tunnels
var (
	PETunnelLinkLocalAddr  = netip.MustParseAddr("fe80::1")
	CPETunnelLinkLocalAddr = netip.MustParseAddr("fe80::2")
)

const TunnelBFDProfile = "maeto-cpe"

const (
	PLRTableID             = 950
	PLRTriggerRulePriority = 100
)

func (r *Reconciler) RenderPEIntent(
	ctx context.Context,
	pe *nodesync.PEIntent,
) (map[string]Resource, error) {
	routeTableCollisionMap := make(map[int]string) // tableID -> tenantID
	resources := make(map[string]Resource)

	mirrorTableID := StringToID("mirror") // as the protector

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

	// This PE as the protector
	if pe.MirrorSID.IsValid() {
		mirrorSID := &SID{
			SIDType:    SIDDT6,
			SID:        pe.MirrorSID,
			VrfTableID: int(mirrorTableID), // tableID (where the SID lives) is on main
			TableID:    unix.RT_TABLE_MAIN,
		}

		resources[mirrorSID.ID().Key] = mirrorSID
	}

	// Used for BFD for CE-PE edge under IPSec tunnel
	xfrmLinkLocalAddr, err := netip.ParsePrefix("fe80::1/64")
	if err != nil {
		return nil, fmt.Errorf("failed to parse xfrm prefix: %w", err)
	}

	// This PE as the PLR
	for _, r := range pe.LFARepairIntent {
		rules := make([]TriggerRule, 0, len(r.Triggers))
		for _, t := range r.Triggers {
			switch t.Kind {
			case nodesync.LFATriggerSIDPrefixEndX:
				// ip -6 route add fc00:0:3:e001::/128 table 950 \
				// encap seg6local action End dev sr0
				sid := &SID{
					SIDType: SIDEnd,
					SID:     t.SIDPrefix.Addr(),
					TableID: PLRTableID,
				}

				resources[sid.ID().Key] = sid

			case nodesync.LFATriggerSIDPrefixService:
				// ip -6 route add fc00:0:5:f000::/52 table 950 \
				// encap seg6 mode encap segs fc00:0:3:e002::,fc00:0:2:e003::,fc00:f:4:: dev eth1
				srRoute := &SRRoute{
					Prefix:   t.SIDPrefix,
					Segments: r.Segments,
					Table:    PLRTableID,
					Color:    0,
				}

				resources[srRoute.ID().Key] = srRoute
			}

			rules = append(rules, TriggerRule{
				Dst:      t.SIDPrefix,
				Table:    PLRTableID,
				Priority: PLRTriggerRulePriority,
			})
		}

		trigger := &RuleTrigger{
			Egress: r.Protecting,
			Rules:  rules,
		}

		resources[trigger.ID().Key] = trigger
	}

	for tenantID, t := range pe.Tenants {
		vrfTableName := fmt.Sprintf("maeto-vrf-%s", tenantID)
		tableID := StringToID(tenantID)

		// Tables that hold the protected pop's tables
		// The mirror table contains replicated End.DT46 SIDs of the protected PE
		// Each End.DT46 decaps to one of the standby table
		standbyVRFTableName := fmt.Sprintf("standby-%s", tenantID)
		standbyVRFTableID := StringToID(standbyVRFTableName)

		vrf := &VRF{
			Name:    vrfTableName,
			TableID: tableID,
		}

		resources[vrf.ID().Key] = vrf

		standbyVRF := &VRF{
			Name:    standbyVRFTableName,
			TableID: standbyVRFTableID,
		}

		resources[standbyVRF.ID().Key] = standbyVRF

		if t.DT46SID.IsValid() {
			dt46SID := &SID{
				SIDType:    SIDDT46,
				SID:        t.DT46SID,
				VrfTableID: int(tableID),
				Metric:     0,
			}

			resources[dt46SID.ID().Key] = dt46SID
		}

		for _, portalIntent := range t.PortalIntents {
			xfrmName := fmt.Sprintf("mto-tun-%s-%d", tenantID, portalIntent.TunnelInterfaceID)

			xfrm := &XFRM{
				Name:          xfrmName,
				IfID:          portalIntent.TunnelInterfaceID,
				Parent:        portalIntent.HostFacingInterface,
				MasterVRF:     vrfTableName,
				LinkLocalAddr: xfrmLinkLocalAddr,
				Index:         0,
			}

			resources[xfrm.ID().Key] = xfrm

			bfdPeer := &BFDPeer{
				Profile:   TunnelBFDProfile,
				VRF:       vrfTableName,
				Interface: xfrmName,
				Peer:      CPETunnelLinkLocalAddr,
			}
			resources[bfdPeer.ID().Key] = bfdPeer

			// vrf owns the XFRM interfaces, not the standby VRF
			// the dataplane checks whether the device is owned by the vrf
			// to not leak routes pointing to ifaces not belonging to the vrf
			// in this case, we WANT that exception to happen i.e leak the route to
			// VRF's device.
			routeTableID := int(tableID)
			allowLeak := false

			if portalIntent.Role != nodesync.PERolePrimary {
				routeTableID = int(standbyVRFTableID)
				allowLeak = true

				// StandbyActive==true when controller acknowledges PE down and CE switchover
				if portalIntent.StandbyActive {
					route := &Route{
						Table: int(tableID),
						Dst:   portalIntent.SitePrefix,
						Dev:   xfrmName,
						Via:   netip.Addr{},
					}

					resources[route.ID().Key] = route
				}
			}

			route := &Route{
				Table:     routeTableID,
				Dst:       portalIntent.SitePrefix,
				Dev:       xfrmName,
				Via:       netip.Addr{},
				Metric:    0,
				AllowLeak: allowLeak,
			}

			resources[route.ID().Key] = route

			for _, limit := range portalLimits(xfrmName, portalIntent.ReservedBandwidth) {
				resources[limit.ID().Key] = limit
			}

			// This PE as the protector
			// Install the mirrored DT46 SID in the standby tables
			if portalIntent.Role == nodesync.PERoleStandby && portalIntent.ProtectedInfo.DT46SID.IsValid() {
				protectedDT46SID := &SID{
					SIDType:    SIDDT46,
					SID:        portalIntent.ProtectedInfo.DT46SID,
					TableID:    int(mirrorTableID),
					VrfTableID: int(standbyVRFTableID),
				}

				resources[protectedDT46SID.ID().Key] = protectedDT46SID
			}
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

				if tenantID, ok := routeTableCollisionMap[routingTableID]; ok {
					if tenantID != path.TenantID { // Routing table collision between two different tenants
						r.logger.ErrorContext(ctx, "found route table ID collision", "tenant", tenantID, "id", routingTableID)

						// Deny installing path if there's a collision
						continue
					}
				}

				srRoute := &SRRoute{
					Prefix:   prefix,
					Segments: reverseSegments,
					Color:    path.Color,
					Table:    routingTableID,
				}

				resources[srRoute.ID().Key] = srRoute
				routeTableCollisionMap[routingTableID] = path.TenantID
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

		// Backup SR routes
		// Low priority SR routes with encap[<DT46>] to allow for egress of protected sites
		for _, e := range t.EgressSites {
			srRoute := &SRRoute{
				Prefix:   e.Prefix,
				Segments: []netip.Addr{e.DT46SID},
				Table:    int(tableID),
				Metric:   2048,
			}

			resources[srRoute.ID().Key] = srRoute
		}
	}

	return resources, nil
}

// Burst of 100ms worth of the rate. Smaller bursts make TCP collapse well under the rate.
const limitBurstSeconds = 0.1

// One limit per reserved class on the portal. The default class (label 0) takes
// everything else, including classes this site has no reservation for.
func portalLimits(iifname string, reserved map[uint64]float64) []*Limit {
	classMarks := make([]uint32, 0, len(reserved))
	for label := range reserved {
		if label != 0 {
			classMarks = append(classMarks, uint32(label))
		}
	}

	limits := make([]*Limit, 0, len(reserved))
	for label, mbps := range reserved {
		rateBytes := uint64(mbps * 125_000)

		limit := &Limit{
			Iifname:    iifname,
			RateBytes:  rateBytes,
			BurstBytes: uint32(float64(rateBytes) * limitBurstSeconds),
		}

		if label == 0 {
			limit.ExceptMarks = classMarks
		} else {
			mark := uint32(label)
			limit.Mark = &mark
		}

		limits = append(limits, limit)
	}

	return limits
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
	limits []DataplaneLimit,
	triggers []DataplaneRuleTrigger,
	bfdPeers []DataplaneBFDPeer,
) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	for _, l := range limits {
		limit := &Limit{
			Iifname:     l.Iifname,
			Mark:        l.Mark,
			ExceptMarks: l.ExceptMarks,
			RateBytes:   l.RateBytes,
			BurstBytes:  l.BurstBytes,
			Handle:      l.Handle,
		}

		resources[limit.ID().Key] = limit
	}

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
			Name:          l.Name,
			IfID:          l.IfID,
			Parent:        l.ParentName,
			MasterVRF:     l.MasterName,
			Index:         l.Index,
			LinkLocalAddr: l.LinkLocalAddr,
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
			Metric: r.Metric,
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
		if r.Dst != nil {
			rule.Dst = r.Dst
		}

		resources[rule.ID().Key] = rule
	}

	for _, s := range sids {
		ip, _ := netip.AddrFromSlice(s.Dst.IP)

		sid := &SID{
			SIDType:    SIDType(s.EncapType),
			SID:        ip,
			TableID:    s.Table,
			VrfTableID: s.VrfTableID,
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
			Metric:   r.Metric,
		}

		resources[srRoute.ID().Key] = srRoute
	}

	for _, p := range bfdPeers {
		peer := &BFDPeer{
			Profile:   p.Profile,
			VRF:       p.VRF,
			Interface: p.Interface,
			Peer:      p.Peer,
		}

		resources[peer.ID().Key] = peer
	}

	for _, t := range triggers {
		trigger := &RuleTrigger{
			Egress: t.Egress,
			Rules:  t.Rules,
		}

		resources[trigger.ID().Key] = trigger
	}

	return resources, nil
}
