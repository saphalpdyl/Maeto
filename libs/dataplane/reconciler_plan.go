package dataplane

import (
	"context"
	"fmt"

	"github.com/saphalpdyl/maeto/libs/nodesync"
)

// Diffs between two node intents to generate
func (r *Reconciler) Plan(ctx context.Context, desired *nodesync.NodeIntent) (map[string]Resource, map[string]Resource, error) {
	// checked against our own role rather than the previous intent, so the very
	// first delivery is validated too
	if desired.NodeType != r.nodeType {
		return nil, nil, fmt.Errorf("intent for node type %q delivered to a %q", desired.NodeType, r.nodeType)
	}

	if r.current != nil {
		// If desired version is greater, disregard generation, reconcile
		// If desired version is the same, and desired generation is greater equal to current generation, reconcile
		// Otherwise, hold off
		if desired.Version < r.current.Version ||
			(desired.Version == r.current.Version && desired.Generation < r.current.Generation) {

			r.logger.DebugContext(ctx, "got stale desired config, skipping")
			return nil, nil, fmt.Errorf(
				"stale desired intent, current: v%d %d, got: v%d %d",
				r.current.Version,
				r.current.Generation,
				desired.Version,
				desired.Generation,
			)
		}
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

	sids, err := r.dp.GetSIDs(nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve sids: %w", err)
	}

	srRoutes, err := r.dp.GetSRRoutes()
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve sr routes: %w", err)
	}

	classifications, err := r.dp.GetClassifications()
	if err != nil {
		return nil, nil, fmt.Errorf("dataplane failure: couldn't retrieve classifications: %w", err)
	}

	currentResources, err := r.RenderFIB(
		ctx,
		vrfLinks,
		xfrmLinks,
		routes,
		sids,
		rules,
		srRoutes,
		classifications,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to render FIB state: %w", err)
	}

	var desiredResources map[string]Resource
	switch i := desired.Intent.(type) {
	case *nodesync.CPEIntent:
		desiredResources, err = r.RenderCPEIntent(ctx, i)
	case *nodesync.PEIntent:
		desiredResources, err = r.RenderPEIntent(ctx, i)
	default:
		return nil, nil, fmt.Errorf("unhandled intent type %T", desired.Intent)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to render %s intent: %w", desired.NodeType, err)
	}

	return currentResources, desiredResources, nil
}
