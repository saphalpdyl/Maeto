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
	maetoagent "github.com/saphalpdyl/maeto/services/maeto-agent"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

const serviceName = "maeto-agent"

func main() {
	ctx := context.Background()

	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		slog.Error("failed to generate service instance id", "error", err)
		os.Exit(1)
	}
	serviceInstanceId := fmt.Sprintf("%x", b)

	config := maetoagent.GetConfig()

	node, err := maetoagent.LoadNode(config.NodePath)
	if err != nil {
		slog.Error("failed to load node identity", "error", err)
		os.Exit(1)
	}

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

	logger := slog.Default().With(log.PopID(node.ID))

	nc, err := nats.Connect(config.NatsConnectURL)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to NATS",
			log.Domain(log.DomainAgentLifecycle),
			slog.String("url", config.NatsConnectURL),
			log.Err(err),
		)
		os.Exit(1)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		logger.ErrorContext(ctx, "failed to get NATS Jetstream context", log.Err(err))
		os.Exit(1)
	}

	defer func() {
		if err := nc.Drain(); err != nil {
			logger.ErrorContext(ctx, "failed to drain NATS", log.Err(err))
		}
	}()

	// TODO: Change to VPP later
	linuxDataplane := maetoagent.NewLinuxShellDataplane()
	agent := maetoagent.NewAgent(node, js, logger.With(log.Domain(log.DomainAgentLifecycle)), linuxDataplane)
	agent.Run(ctx)
}
