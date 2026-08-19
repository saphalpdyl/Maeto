package maetoagent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// shared by every dataplane implementation that has to fall back to a command,
// so none of them depends on another's type

func run(ctx context.Context, name string, args ...string) error {
	//nolint:gosec // G204: Shell wrapper requires variable execution path
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func runWithOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	//nolint:gosec // G204: Shell wrapper requires variable execution path
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return out, nil
}
