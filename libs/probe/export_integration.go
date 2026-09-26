//go:build integration

package probe

import "context"

func (s *Supervisor) MarkStartedForTest() {
	s.markStarted()
}

func (s *Supervisor) ReconcileWithTarget(ctx context.Context, target map[string]ProbeConfig) error {
	return s.reconcileWithTarget(ctx, target)
}
