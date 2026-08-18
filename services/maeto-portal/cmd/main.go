package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/telemetry"
	maetoportal "github.com/saphalpdyl/maeto/services/maeto-portal"
	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
)

const serviceName = "maeto-portal"

func main() {
	ctx := context.Background()

	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		slog.Error("failed to generate service instance id", "error", err)
		os.Exit(1)
	}
	serviceInstanceId := fmt.Sprintf("%x", b)

	config := maetoportal.GetConfig()

	logShutdown, err := telemetry.SetupLogging(ctx, config.OtelSink, config.OtelEndpoint, serviceName, serviceInstanceId, false)
	if err != nil {
		slog.Error("failed to setup logging", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := logShutdown(ctx); err != nil {
			slog.Error("failed to shut down logging", "error", err)
		}
	}()

	traceShutdown, err := telemetry.SetupTracing(ctx, config.OtelSink, config.OtelEndpoint, serviceName, serviceInstanceId, false)
	if err != nil {
		slog.Error("failed to setup tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := traceShutdown(ctx); err != nil {
			slog.Error("failed to shut down tracing", "error", err)
		}
	}()

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.Default().With(
		log.InstanceID(serviceInstanceId),
		log.PortalID(config.PortalID),
	)

	nc, err := nats.Connect(config.NatsConnectURL)
	if err != nil {
		slog.ErrorContext(ctx, "failed to connect to nats", log.Err(err))
		os.Exit(1)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create jetstream context", log.Err(err))
		os.Exit(1)
	}

	portal := maetoportal.NewPortal(
		serviceInstanceId,
		js,
		config,
		logger.With(log.Domain(log.DomainPortalLifecycle)),
	)

	if err := portal.Run(ctx); err != nil {
		slog.ErrorContext(ctx, "portal exited with error", log.Err(err))
		os.Exit(1)
	}

	slog.InfoContext(ctx, "shutting down")
}
