package dataplane

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"net/netip"

	"github.com/saphalpdyl/maeto/libs/dataplane/log"
)

type Reconciler struct {
	dp       Dataplane
	nodeType NodeType
	current  *NodeIntent

	intentFeed <-chan *NodeIntent

	logger *slog.Logger
}

func NewReconciler(dp Dataplane, nodeType NodeType, logger *slog.Logger, intentFeed <-chan *NodeIntent) *Reconciler {
	return &Reconciler{
		dp:         dp,
		nodeType:   nodeType,
		intentFeed: intentFeed,
		logger:     logger,
	}
}

func (r *Reconciler) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case intent := <-r.intentFeed:
			r.logger.InfoContext(ctx, "reconciler received intent", "intent", intent)
			if err := r.Reconcile(ctx, intent); err != nil {
				r.logger.ErrorContext(ctx, "reconcile failed", log.Err(err))
			}
		}
	}
}

// RenderCPEIntent builds the cpe side: one tunnel interface, a default route
// into it in maeto's own table, and a rule sending this site's traffic there.
func (r *Reconciler) RenderCPEIntent(ctx context.Context, intent *CPEIntent) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	if intent.TunnelInterfaceID == 0 {
		r.logger.WarnContext(ctx, "tunnel iface id == 0: intent is not ready to be installed")
		return resources, nil
	}

	if !intent.SitePrefix.IsValid() {
		return nil, fmt.Errorf("cpe intent has no site prefix, nothing could select the tunnel")
	}

	xfrmTunnelName := fmt.Sprintf("xfrm-tenant-%s", intent.TenantID)
	xfrm := &XFRM{
		Name:      xfrmTunnelName,
		IfID:      intent.TunnelInterfaceID,
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
		Src:      intent.SitePrefix,
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

func (r *Reconciler) RenderPE(ctx context.Context, intent *PEIntent) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	for tenantID, t := range intent.Tenants {
		vrfTableName := fmt.Sprintf("maeto-vrf-%s", tenantID)
		tableID := StringToID(tenantID)

		vrf := &VRF{
			Name:    vrfTableName,
			TableID: tableID,
			Index:   0,
		}

		resources[vrf.ID().Key] = vrf

		for _, portalIntent := range t {
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

			dt46SID := &SID{
				SIDType: SIDDT46,
				SID:     portalIntent.DT46SID,
				TableID: int(tableID),
				Metric:  0,
			}

			resources[dt46SID.ID().Key] = dt46SID
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

	return resources, nil
}

// Diffs between two node intents to generate
func (r *Reconciler) Plan(ctx context.Context, desired *NodeIntent) (map[string]Resource, map[string]Resource, error) {
	// checked against our own role rather than the previous intent, so the very
	// first delivery is validated too
	if desired.NodeType != r.nodeType {
		return nil, nil, fmt.Errorf("intent for node type %q delivered to a %q", desired.NodeType, r.nodeType)
	}

	// TODO: Same generation should be looked up against FIB to allow for clean states
	if (r.current != nil) && (desired.Generation < r.current.Generation) {
		r.logger.DebugContext(ctx, "got stale desired config, skipping")
		return nil, nil, fmt.Errorf("stale desired intent, current: %d, got: %d", r.current.Generation, desired.Generation)
	}

	// Fetch FIB state
	rVrfs, err := r.dp.GetLinksByType("vrf")
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve vrf links")
	}
	vrfLinks := make([]*DataplaneVRF, len(rVrfs))
	for i, v := range rVrfs {
		v, ok := v.(*DataplaneVRF)
		if !ok {
			return nil, nil, fmt.Errorf("failed to parse DataplaneLink as DataplaneVRF")
		}
		vrfLinks[i] = v
	}

	rXfrms, err := r.dp.GetLinksByType("xfrm")
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve xfrm links")
	}
	xfrmLinks := make([]*DataplaneXFRM, len(rXfrms))
	for i, x := range rXfrms {
		x, ok := x.(*DataplaneXFRM)
		if !ok {
			return nil, nil, fmt.Errorf("failed to parse DataplaneLink as DataplaneXFRM")
		}
		xfrmLinks[i] = x
	}

	routes, err := r.dp.GetPrefixRoutes()
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve prefix routes: %w", err)
	}

	rules, err := r.dp.GetRules()
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve rules: %w", err)
	}

	sids, err := r.dp.GetSIDs()
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve sids: %w", err)
	}

	currentResources, err := r.RenderFIB(
		ctx,
		vrfLinks,
		xfrmLinks,
		routes,
		sids,
		rules,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to render FIB state: %w", err)
	}

	var desiredResources map[string]Resource
	switch intent := desired.Intent.(type) {
	case *CPEIntent:
		desiredResources, err = r.RenderCPEIntent(ctx, intent)
	case *PEIntent:
		desiredResources, err = r.RenderPE(ctx, intent)
	default:
		return nil, nil, fmt.Errorf("unhandled intent type %T", desired.Intent)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to render %s intent: %w", desired.NodeType, err)
	}

	return currentResources, desiredResources, nil
}

type DiffResult struct {
	Remove []Resource
	Add    []Resource
}

func keyDifference[K comparable, V1, V2 any](map1 map[K]V1, map2 map[K]V2) []K {
	var diff []K
	for k := range map1 {
		if _, exists := map2[k]; !exists {
			diff = append(diff, k)
		}
	}
	return diff
}

func (r *Reconciler) Diff(ctx context.Context, curr map[string]Resource, desired map[string]Resource) (DiffResult, error) {
	remove := keyDifference(curr, desired)
	add := keyDifference(desired, curr)

	removeResources := make([]Resource, 0)
	addResources := make([]Resource, 0)

	for _, k := range remove {
		removeItem := curr[k]
		removeResources = append(removeResources, removeItem)
	}

	for _, k := range add {
		addItem := desired[k]
		addResources = append(addResources, addItem)
	}

	return DiffResult{
		Remove: removeResources,
		Add:    addResources,
	}, nil
}

func (r *Reconciler) Apply(ctx context.Context, diff DiffResult) error {
	// First of all, we remove shit
	// We remove from bottom up i.e route -> xfrm -> vrf
	// TODO: Later group by kind in a map so that we don't traverse everything everytime
	for _, kind := range []Kind{KindRule, KindSID, KindRoute, KindXFRM, KindVRF} {
		for _, resource := range diff.Remove {
			if resource.ID().Kind != kind {
				continue
			}

			switch res := resource.(type) {
			case *Route:
				if err := r.dp.RemovePrefixRoute(res.Dst, res.Table); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove route", log.Err(err))
					break
				}

			case *XFRM:
				if err := r.dp.RemoveXFRMInterface(res.Index); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove xfrm interface", log.Err(err))
					break
				}

			case *VRF:
				if err := r.dp.RemoveVRF(res.Index); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove vrf", log.Err(err))
					break
				}

			case *Rule:
				if err := r.dp.RemoveRule(res.Priority, res.Src, res.Table); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove rule", log.Err(err))
					break
				}
			case *SID:
				if err := r.dp.RemoveSID(res.SID); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove sid", log.Err(err))
					break
				}
			}

		}
	}

	// Addition
	for _, kind := range []Kind{KindVRF, KindXFRM, KindRoute, KindSID, KindRule} {
		for _, resource := range diff.Add {
			if resource.ID().Kind != kind {
				continue
			}

			switch res := resource.(type) {
			case *VRF:
				if err := r.dp.UpsertVRF(res.Name, int(res.TableID)); err != nil {
					r.logger.ErrorContext(ctx, "failed to upsert VRF", log.Err(err))
				}

			case *XFRM:
				mVRF := (*string)(nil)
				if res.MasterVRF != "" {
					mVRF = &res.MasterVRF // During rendering, masterVRF is set to "" for CPEs
				}

				parent := res.Parent
				if parent == "" {
					parent = "lo"
				}

				if err := r.dp.InsertXFRMInterface(res.Name, parent, res.IfID, mVRF); err != nil {
					r.logger.ErrorContext(ctx, "failed to insert XFRM", log.Err(err))
				}

			case *Route:
				if err := r.dp.InsertPrefixRoute(res.Dev, res.Table, res.Dst, res.Via); err != nil {
					r.logger.ErrorContext(ctx, "failed to insert Route", log.Err(err))
				}

			case *Rule:
				if err := r.dp.UpsertRule(res.Priority, res.Src, res.Table); err != nil {
					r.logger.ErrorContext(ctx, "failed to upsert rule", log.Err(err))
				}

			case *SID:
				if res.SIDType != SIDDT46 {
					r.logger.WarnContext(ctx, "SID Type is not supported", slog.String("sid_type", string(res.SIDType)))
				}

				if err := r.dp.UpsertDT46SID(res.SID, res.TableID); err != nil {
					r.logger.ErrorContext(ctx, "failed to upsert DT46 SID", log.Err(err))
				}
			}

		}
	}

	return nil
}

func (r *Reconciler) Reconcile(ctx context.Context, intent *NodeIntent) error {
	const maxPasses = 3

	for pass := 0; pass < maxPasses; pass++ {
		current, desired, err := r.Plan(ctx, intent)
		r.logger.InfoContext(ctx, "reconciling", slog.Any("current", current), slog.Any("desired", desired))
		if err != nil {
			r.logger.ErrorContext(ctx, "failed to plan reconciliation", log.Err(err))
			return err
		}

		result, err := r.Diff(ctx, current, desired)
		if err != nil {
			r.logger.ErrorContext(ctx, "failed to diff reconciliation", log.Err(err))
			return err
		}

		if (len(result.Add) == 0) && (len(result.Remove) == 0) {
			r.logger.InfoContext(
				ctx,
				fmt.Sprintf("converged on pass %d, where pass = 0 means first pass", pass),
				slog.Int("pass", pass),
				slog.Int("desired", len(desired)),
				slog.Int("current", len(current)),
			)

			r.current = intent
			return nil // converged
		}

		err = r.Apply(ctx, result)
		if err != nil {
			r.logger.ErrorContext(ctx, "failed to apply reconciliatin resources", log.Err(err))
			return err
		}
	}

	return fmt.Errorf("dataplane could not converged after %d passes", maxPasses)
}
