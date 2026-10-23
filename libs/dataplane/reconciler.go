package dataplane

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/saphalpdyl/maeto/libs/dataplane/log"
	"github.com/saphalpdyl/maeto/libs/nodesync"
)

type Reconciler struct {
	dp       Dataplane
	nodeType nodesync.NodeType
	current  *nodesync.NodeIntent

	intentFeed <-chan *nodesync.NodeIntent

	reporter StateReporter

	// For reporting
	lastCurrent map[string]Resource
	lastDesired map[string]Resource

	logger *slog.Logger
}

func (r *Reconciler) SetStateReporter(reporter StateReporter) {
	r.reporter = reporter
}

func NewReconciler(dp Dataplane, nodeType nodesync.NodeType, logger *slog.Logger, intentFeed <-chan *nodesync.NodeIntent) *Reconciler {
	return &Reconciler{
		dp:         dp,
		nodeType:   nodeType,
		intentFeed: intentFeed,
		logger:     logger,
	}
}

func (r *Reconciler) Start(ctx context.Context) error {
	// For Linux dataplane, since plan tries to get classifications
	// before the render apply resulting in an ENOENT error
	err := r.dp.EnsureClassificationBase()
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ni := <-r.intentFeed:
			r.logger.InfoContext(ctx, "reconciler received intent", "intent", ni)
			if err := r.Reconcile(ctx, ni); err != nil {
				r.logger.ErrorContext(ctx, "reconcile failed", log.Err(err))
			}
		}
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, ni *nodesync.NodeIntent) (err error) {
	const maxPasses = 3

	var (
		current, desired map[string]Resource
		result           DiffResult
		converged        bool
		passes           int
	)

	defer func() {
		r.report(ctx, ni, current, desired, result, converged, passes, err)
	}()

	for pass := 0; pass < maxPasses; pass++ {
		passes = pass + 1

		current, desired, err = r.Plan(ctx, ni)
		r.logger.InfoContext(ctx, "reconciling", slog.Any("current", current), slog.Any("desired", desired))
		if err != nil {
			r.logger.ErrorContext(ctx, "failed to plan reconciliation", log.Err(err))
			return err
		}

		result, err = r.Diff(ctx, current, desired)
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

			r.current = ni
			converged = true

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

func (r *Reconciler) report(
	ctx context.Context,
	ni *nodesync.NodeIntent,
	current, desired map[string]Resource,
	result DiffResult,
	converged bool,
	passes int,
	reconcileErr error,
) {
	if r.reporter == nil {
		return
	}

	observed := current != nil
	if observed {
		r.lastCurrent = current
		r.lastDesired = desired
	} else {
		current = r.lastCurrent
		desired = r.lastDesired
	}

	state := &NodeState{
		NodeType:   r.nodeType,
		ReportedAt: time.Now(),
		Observed:   observed,
		Converged:  converged,
		Passes:     passes,
	}

	if ni != nil {
		state.Generation = ni.Generation
		state.NodeID = intentNodeID(ni)
	}

	if reconcileErr != nil {
		state.Error = reconcileErr.Error()
	}

	var err error
	if state.Current, err = ResourceStatesFromMap(current); err != nil {
		r.logger.ErrorContext(ctx, "failed to encode current state", log.Err(err))
		return
	}

	if state.Desired, err = ResourceStatesFromMap(desired); err != nil {
		r.logger.ErrorContext(ctx, "failed to encode desired state", log.Err(err))
		return
	}

	if state.Add, err = ResourceStates(result.Add); err != nil {
		r.logger.ErrorContext(ctx, "failed to encode added state", log.Err(err))
		return
	}

	if state.Remove, err = ResourceStates(result.Remove); err != nil {
		r.logger.ErrorContext(ctx, "failed to encode removed state", log.Err(err))
		return
	}

	if err := r.reporter.Report(ctx, state); err != nil {
		r.logger.ErrorContext(ctx, "failed to report node state", log.Err(err))
	}
}

func intentNodeID(ni *nodesync.NodeIntent) string {
	switch i := ni.Intent.(type) {
	case *nodesync.PEIntent:
		return i.NodeID
	case *nodesync.CPEIntent:
		return i.PortalID
	default:
		return ""
	}
}
