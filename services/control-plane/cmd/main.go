package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/saphalpdyl/maeto/libs/telemetry"
	controlplane "github.com/saphalpdyl/maeto/services/control-plane"
	log "github.com/saphalpdyl/maeto/services/control-plane/log"
)

func main() {
	ctx := context.Background()

	b := make([]byte, 4)
	rand.Read(b)
	serviceInstanceId := fmt.Sprintf("%x", b)

	config := controlplane.GetConfig()

	logShutdown, err := telemetry.SetupLogging(ctx, config.OtelSink, config.OtelEndpoint, "control-plane", serviceInstanceId, false)
	if err != nil {
		slog.Error("failed to setup logging", log.Err(err))
		os.Exit(1)
	}
	defer func() {
		if err := logShutdown(ctx); err != nil {
			slog.Error("failed to shut down logging", log.Err(err))
		}
	}()

	traceShutdown, err := telemetry.SetupTracing(ctx, config.OtelSink, config.OtelEndpoint, "control-plane", serviceInstanceId, false)
	if err != nil {
		slog.Error("failed to setup tracing", log.Err(err))
		os.Exit(1)
	}
	defer func() {
		if err := traceShutdown(ctx); err != nil {
			slog.Error("failed to shut down tracing", log.Err(err))
		}
	}()

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.Default().With(log.Domain(log.DomainControlPlaneLifecycle))

	logger.InfoContext(ctx, "starting control plane", log.ListenAddress(config.ListenAddress))

	<-ctx.Done()

	logger.InfoContext(ctx, "shutting down")
}
