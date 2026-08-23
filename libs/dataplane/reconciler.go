package dataplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/saphalpdyl/maeto/libs/dataplane/log"
	"golang.org/x/sys/unix"
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

func (r *Reconciler) RenderCPEIntent(ctx context.Context, intent *CPEIntent) (map[string]Resource, error) {
	resources := make(map[string]Resource)

	dRoute, dDev, err := r.dp.GetDefaultRouteAndDev()
	if err != nil {
		return nil, fmt.Errorf("dataplane failure: couldn't retrieve default route and device")
	}

	// No VRF tables for CPE

	// There is just one interface on cpe

	if intent.TunnelInterfaceID == 0 {
		r.logger.WarnContext(ctx, "tunnel iface id == 0: intent is not ready to be installed")
		return resources, nil
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

	peEndpointAddrPrefix, err := intent.TunnelPEEndpointAddr.Prefix(128)
	if err != nil {
		return nil, fmt.Errorf("failed to create prefix from PE endpoint addr: %w", err)
	}

	gwAddr, err := netip.ParseAddr(dRoute)
	if err != nil {
		return nil, fmt.Errorf("failed to parse default route addr: %w", err)
	}

	espExcludeRoute := &Route{
		Table:  unix.RT_TABLE_MAIN, // TODO: Linux impl leak
		Dst:    peEndpointAddrPrefix,
		Dev:    dDev,
		Via:    gwAddr,
		Metric: 0,
	}

	resources[espExcludeRoute.ID().Key] = espExcludeRoute

	// TODO: Exclude NATS endpoint as well
	natsExcludePrefix, err := netip.ParsePrefix("3fff:172:20:20::800:11/128")
	if err != nil {
		return nil, fmt.Errorf("failed to parse NATS exclude prefix: %w", err)
	}

	natsExcludeRoute := &Route{
		Table:  0,
		Dst:    natsExcludePrefix,
		Dev:    dDev,
		Via:    gwAddr,
		Metric: 0,
	}
	resources[natsExcludeRoute.ID().Key] = natsExcludeRoute

	defaultExcludeRoute := &Route{
		Table:  0,
		Dst:    netip.PrefixFrom(netip.IPv6Unspecified(), 0), // ::/0
		Dev:    xfrmTunnelName,
		Via:    netip.Addr{},
		Metric: 0,
	}

	resources[defaultExcludeRoute.ID().Key] = defaultExcludeRoute

	return resources, nil
}

func (r *Reconciler) RenderPE(ctx context.Context, intent *PEIntent) (map[string]Resource, error) {
	return nil, nil
}

func (r *Reconciler) RenderFIB(
	ctx context.Context,
	dRoute string,
	dDev string,
	vrfLinks []*DataplaneVRF,
	xfrmLinks []*DataplaneXFRM,
	routes []DataplaneRoute,
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
			Parent:    fmt.Sprint(l.ParentIndex),
			MasterVRF: fmt.Sprint(l.MasterIndex),
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
			Dev:    dDev,
			Via:    viaip,
			Metric: 0,
		}

		resources[route.ID().Key] = route
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
	defaultRoute, defaultDev, err := r.dp.GetDefaultRouteAndDev()
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve default route and device")
	}

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
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve prefix routes")
	}

	currentResources, err := r.RenderFIB(
		ctx,
		defaultRoute,
		defaultDev,
		vrfLinks,
		xfrmLinks,
		routes,
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
	for _, kind := range []Kind{KindRoute, KindXFRM, KindVRF} {
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
			}
		}
	}

	// Addition
	for _, kind := range []Kind{KindVRF, KindXFRM, KindRoute} {
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

				if err := r.dp.InsertXFRMInterface(res.Name, "lo", res.IfID, mVRF); err != nil {
					r.logger.ErrorContext(ctx, "failed to insert XFRM", log.Err(err))
				}

			case *Route:
				if err := r.dp.InsertPrefixRoute(res.Dev, 0, res.Dst); err != nil {
					r.logger.ErrorContext(ctx, "failed to insert Route", log.Err(err))
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
