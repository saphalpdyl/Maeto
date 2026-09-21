//go:build integration

package probe

import "context"

func (s *Supervisor) SetBaseContextForTest(ctx context.Context) {
	s.setBaseContext(ctx)
}

func (s *Supervisor) ReconcileWithTarget(target map[string]ProbeConfig) error {
	return s.reconcileWithTarget(target)
}
