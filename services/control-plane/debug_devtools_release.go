// debug_devtools_release.go
//go:build !devtools

package controlplane

import (
	"context"
	"log/slog"
)

func (c *Controller) startDebugTools(_ context.Context, _ *slog.Logger) {
}
