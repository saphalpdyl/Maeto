package dataplane

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"net/netip"
	"time"

	"github.com/saphalpdyl/maeto/libs/dataplane/log"
)

type NodeType string

const (
	NodeTypeCPE NodeType = "cpe"
	NodeTypePE  NodeType = "pe"
)

type NodeIntent struct {
	NodeType   NodeType  `json:"node_type"`
	Intent     Intent    `json:"intent"`
	Timestamp  time.Time `json:"timestamp"`
	Generation uint32    `json:"generation"`
	Version    uint32    `json:"version"`
}

// the wire form. json.RawMessage exists only here: NodeIntent.Intent is a typed
// value everywhere else, so nothing outside this file decodes an intent.
type nodeIntentWire struct {
	NodeType   NodeType        `json:"node_type"`
	Intent     json.RawMessage `json:"intent"`
	Timestamp  time.Time       `json:"timestamp"`
	Generation uint32          `json:"generation"`
	Version    uint32          `json:"version"`
}

// MarshalJSON is not needed: the Intent interface marshals its concrete value,
// so the encoded shape is identical to the wire struct above.
func (n *NodeIntent) UnmarshalJSON(b []byte) error {
	var w nodeIntentWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}

	n.NodeType = w.NodeType
	n.Timestamp = w.Timestamp
	n.Generation = w.Generation
	n.Version = w.Version

	switch w.NodeType {
	case NodeTypeCPE:
		var i CPEIntent
		if err := json.Unmarshal(w.Intent, &i); err != nil {
			return fmt.Errorf("decode cpe intent: %w", err)
		}
		n.Intent = &i
	case NodeTypePE:
		var i PEIntent
		if err := json.Unmarshal(w.Intent, &i); err != nil {
			return fmt.Errorf("decode pe intent: %w", err)
		}
		n.Intent = &i
	default:
		return fmt.Errorf("unknown node type %q", w.NodeType)
	}

	return nil
}

// Clone is on the interface so a new variant cannot forget to deep copy
// whatever it holds.
type Intent interface {
	isIntent()
	Clone() Intent
}

func (*CPEIntent) isIntent() {}
func (*PEIntent) isIntent()  {}

// Clone deep copies the intent so a publish can marshal outside the registry
// lock without racing a concurrent mutation through the stored pointer.
func (n *NodeIntent) Clone() *NodeIntent {
	if n == nil {
		return nil
	}

	out := *n
	if n.Intent != nil {
		out.Intent = n.Intent.Clone()
	}

	return &out
}

// every field is a value type, so the struct copy is the deep copy
func (i *CPEIntent) Clone() Intent {
	if i == nil {
		return nil
	}

	out := *i

	return &out
}

func (i *PEIntent) Clone() Intent {
	if i == nil {
		return nil
	}

	out := *i

	// Tenants is two levels of map, and a struct copy would share both
	out.Tenants = make(map[string]map[string]PE_PortalIntent, len(i.Tenants))
	for tenantID, portals := range i.Tenants {
		copied := make(map[string]PE_PortalIntent, len(portals))
		for portalID, portal := range portals {
			copied[portalID] = portal // PE_PortalIntent is all value types
		}
		out.Tenants[tenantID] = copied
	}

	return &out
}

// High-level intent instructions that get converted to dataplane instructions
// by the reconciler
type CPEIntent struct {
	PortalID             string       `json:"portal_id"`
	TunnelInterfaceID    uint32       `json:"tunnel_interface_id"`
	TunnelPE             string       `json:"tunnel_pe"`
	TunnelPEEndpointAddr netip.Addr   `json:"tunnel_pe_endpoint_addr"`
	TenantID             string       `json:"tenant_id"`
	TenantPrefix         netip.Prefix `json:"tenant_prefix"`
	SitePrefix           netip.Prefix `json:"site_prefix"`
}

// Bad naming convention: Portal-specific configuration done on PE's dataplane
type PE_PortalIntent struct {
	HostFacingInterface string `json:"host_facing_interface"`
	TunnelInterfaceID   uint32 `json:"tunnel_interface_id"`

	SitePrefix   netip.Prefix `json:"site_prefix"`
	TenantPrefix netip.Prefix `json:"tenant_prefix"`
}

type PEIntent struct {
	NodeID string `json:"node_id"`

	// Multiple tenants can have multiple sites. One site is represented in Maeto by
	// the presence of only ONE maeto-portal device
	//
	// 1st key = tenant_id
	//
	// 2nd key = portal_id
	Tenants map[string]map[string]PE_PortalIntent `json:"tenants"`
}

type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionReplace Action = "replace"
	ActionDelete  Action = "delete"
	ActionNone    Action = "none"
)

type Kind string

const (
	KindVRF   Kind = "vrf"
	KindRule  Kind = "rule"
	KindXFRM  Kind = "xfrm"
	KindRoute Kind = "route"
)

type ID struct {
	Kind Kind
	Key  string // "vrf-tenant-231" | "maeto-tun0" | "254|::/0"
}

// The shared neutral that PEIntent, CPEIntent and the FIB state convert to before
// diffing.
type Resource interface {
	ID() ID
	// only the concrete type knows which of its fields can changplace
	CompareTo(actual Resource) Action // ActionNone | ActionUpdatActionReplace
	Type() string
}

// need for delete + create
type VRF struct {
	Name    string `rc:"id"`
	TableID uint32 `rc:"id"`
	Index   int    // observed only
}

func (v *VRF) ID() ID {
	return ID{
		Kind: KindVRF,
		Key:  v.Name,
	}
}

func (v *VRF) Type() string { return "vrf" }

func (v *VRF) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}
	if a, ok := actual.(*VRF); ok {
		if v.Name != a.Name || v.TableID != a.TableID {
			return ActionReplace
		}
		return ActionNone
	}
	return ActionReplace
}

// need for delete + create
type XFRM struct {
	Name      string `rc:"id"`
	IfID      uint32 `rc:"id"`
	Parent    string
	MasterVRF string `rc:"mut"`
	Index     int    // observed only
}

func (x *XFRM) ID() ID {
	return ID{
		Kind: KindXFRM,
		Key:  fmt.Sprintf("%s.%d", x.Name, x.IfID),
	}
}

func (x *XFRM) Type() string { return "xfrm" }

func (x *XFRM) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}
	if a, ok := actual.(*XFRM); ok {
		if x.Name != a.Name || x.IfID != a.IfID || x.Parent != a.Parent || x.MasterVRF != a.MasterVRF {
			return ActionReplace
		}
		return ActionNone
	}
	return ActionReplace
}

// A cpe carries one tenant, so one table and one rule priority is enough. Both
// are maeto's alone: main stays owned by whatever manages the wan.
const (
	CPETunnelTable  = 100
	CPERulePriority = 100
)

// need for delete + create: a rule has no mutable field, every one identifies it
type Rule struct {
	Priority int          `rc:"id"`
	Src      netip.Prefix `rc:"id"`
	Table    int          `rc:"id"`
}

func (r *Rule) ID() ID {
	return ID{
		Kind: KindRule,
		Key:  fmt.Sprintf("%d.%s.%d", r.Priority, r.Src.String(), r.Table),
	}
}

func (r *Rule) Type() string { return "rule" }

func (r *Rule) CompareTo(actual Resource) Action {
	a, ok := actual.(*Rule)
	if !ok {
		return ActionReplace
	}
	if r.Priority != a.Priority || r.Src != a.Src || r.Table != a.Table {
		return ActionReplace
	}

	return ActionNone
}

// Ideompotently replaced. no need for delete+create
type Route struct {
	Table  int          `rc:"id"`
	Dst    netip.Prefix `rc:"id"`
	Dev    string       `rc:"mut"`
	Via    netip.Addr   `rc:"mut"`
	Metric int
}

func (r *Route) ID() ID {
	return ID{
		Kind: KindRoute,
		Key:  fmt.Sprintf("%d.%s.%s", r.Table, r.Dst.String(), r.Dev),
	}
}

func (r *Route) Type() string { return "route" }

func (r *Route) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}
	if a, ok := actual.(*Route); ok {
		if r.Table != a.Table || r.Dst != a.Dst || r.Dev != a.Dev || r.Via != a.Via || r.Metric != a.Metric {
			return ActionReplace
		}
		return ActionNone
	}
	return ActionReplace
}

////////////////////////////////////////////////////////////////

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
//
// Nothing is written to the main table. That table belongs to whatever manages
// the wan -- dhcp, ra, pppd -- and esp packets plus the control plane
// connection both source from the underlay address, so they never match the
// rule and keep following the real default. That removes the need for endpoint
// exclusions entirely, and with them the read of a route we were about to
// replace.
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
		}
	}

	return resources, nil
}

func (r *Reconciler) RenderFIB(
	ctx context.Context,
	vrfLinks []*DataplaneVRF,
	xfrmLinks []*DataplaneXFRM,
	routes []DataplaneRoute,
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

	for _, rule := range rules {
		res := &Rule{Priority: rule.Priority, Src: rule.Src, Table: rule.Table}
		resources[res.ID().Key] = res
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

	currentResources, err := r.RenderFIB(
		ctx,
		vrfLinks,
		xfrmLinks,
		routes,
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
	for _, kind := range []Kind{KindRule, KindRoute, KindXFRM, KindVRF} {
		for _, resource := range diff.Remove {
			if resource.ID().Kind != kind {
				continue
			}

			switch res := resource.(type) {
			case *Route:
				if err := r.dp.RemovePrefixRoute(res.Dst, res.Table); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove route", log.Err(err))
				}

			case *XFRM:
				if err := r.dp.RemoveXFRMInterface(res.Index); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove xfrm interface", log.Err(err))
				}

			case *VRF:
				if err := r.dp.RemoveVRF(res.Index); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove vrf", log.Err(err))
				}

			case *Rule:
				if err := r.dp.RemoveRule(res.Priority, res.Src, res.Table); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove rule", log.Err(err))
				}
			}
		}
	}

	// Addition
	for _, kind := range []Kind{KindVRF, KindXFRM, KindRoute, KindRule} {
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
