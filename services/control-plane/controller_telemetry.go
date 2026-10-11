package controlplane

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

func (c *Controller) startTelemetryCollection(ctx context.Context) {
	if err := c.ensureProbeResultStream(ctx); err != nil {
		c.logger.ErrorContext(ctx, "failed to ensure probe result stream", log.Err(err))
		return
	}

	for _, collector := range c.telemetryCollectors {
		if err := collector.Start(ctx); err != nil {
			c.logger.ErrorContext(ctx, "failed to start telemetry collector", log.Err(err))
			continue
		}

		c.logger.InfoContext(ctx, fmt.Sprintf("telemetry collector started successfully, consuming at %s", collector.SubjectPrefix))
	}

	go func() {
		<-ctx.Done()

		for _, collector := range c.telemetryCollectors {
			collector.Stop()
		}
	}()
}

// ensureProbeResultStream owns the shared probe result stream. Every agent
// publishes under its own node subtree and every collector filters on one, so
// the subject list has to span them all and only one writer can hold it.
func (c *Controller) ensureProbeResultStream(ctx context.Context) error {
	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        probe.ProbeResultStream,
		Description: "raw probe results published by maeto agents",
		Subjects:    []string{probe.ProbeResultSubjectPrefix + ".>"},
		Storage:     jetstream.FileStorage,
		MaxAge:      ProbeResultMaxAge,
	})
	if err != nil {
		return fmt.Errorf("failed to create stream %s: %w", probe.ProbeResultStream, err)
	}

	return nil
}
