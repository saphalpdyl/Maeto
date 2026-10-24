package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/avast/retry-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/observability"
	"github.com/saphalpdyl/maeto/libs/store"
	controlplane "github.com/saphalpdyl/maeto/services/control-plane"
	log "github.com/saphalpdyl/maeto/services/control-plane/log"
	"golang.org/x/sync/errgroup"
)

func main() {
	ctx := context.Background()

	b := make([]byte, 4)
	rand.Read(b)
	serviceInstanceId := fmt.Sprintf("%x", b)

	config := controlplane.GetConfig()

	logShutdown, err := observability.SetupLogging(ctx, config.OtelSink, config.OtelEndpoint, "control-plane", serviceInstanceId, false)
	if err != nil {
		slog.Error("failed to setup logging", log.Err(err))
		os.Exit(1)
	}
	defer func() {
		if err := logShutdown(ctx); err != nil {
			slog.Error("failed to shut down logging", log.Err(err))
		}
	}()

	traceShutdown, err := observability.SetupTracing(ctx, config.OtelSink, config.OtelEndpoint, "control-plane", serviceInstanceId, false)
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

	///// External service connections
	var nc *nats.Conn
	var js jetstream.JetStream
	var dbStore *store.DataStore
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		nc, js, err = tryConnectToNATS(gCtx, js, nc, config, logger)
		return err
	})

	g.Go(func() error {
		dbStore, err = tryConnectToDB(gCtx, dbStore, config, logger)
		return err
	})

	if err := g.Wait(); err != nil {
		logger.ErrorContext(ctx, "failed to start control plane", log.Err(err))
		os.Exit(1)
	}
	///// External service connections

	cp, err := controlplane.NewController(ctx, js, dbStore, config, logger)
	if err != nil {
		logger.ErrorContext(ctx, "failed to initialize controller", log.Err(err))
		os.Exit(1)
	}

	cp.Start(ctx)

	<-ctx.Done()

	logger.InfoContext(ctx, "shutting down")
	if err = nc.Drain(); err != nil {
		logger.ErrorContext(ctx, "failed to drain NATS", log.Err(err))
	}

}

func tryConnectToDB(ctx context.Context, dbStore *store.DataStore, config controlplane.Config, logger *slog.Logger) (*store.DataStore, error) {
	err := retry.Do(func() error {
		var err error
		dbStore, err = store.OpenDataStore(ctx, config.DatabaseURL)
		if err != nil {
			logger.ErrorContext(ctx, "failed to connect to database", log.Err(err))
			return err
		}

		return nil
	},
		retry.Context(ctx),
		retry.Attempts(10),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(n uint, err error) {
			logger.WarnContext(ctx, "retrying NATS connection", log.Err(err))
		}),
	)
	if err != nil {
		return nil, err
	}

	return dbStore, nil
}

func tryConnectToNATS(ctx context.Context, js jetstream.JetStream, nc *nats.Conn, config controlplane.Config, logger *slog.Logger) (*nats.Conn, jetstream.JetStream, error) {
	err := retry.Do(func() error {
		var err error

		// the lab's nats is torn down and rebuilt by make ap, which can take
		// longer than the default 60 x 2s before the client gives up for good
		nc, err = nats.Connect(config.NatsConnectURL,
			nats.MaxReconnects(-1),
			nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
				logger.WarnContext(ctx, "nats disconnected", log.Err(err))
			}),
			nats.ClosedHandler(func(_ *nats.Conn) {
				logger.ErrorContext(ctx, "nats connection closed")
			}),
		)
		if err != nil {
			logger.ErrorContext(ctx, "failed to connect to NATS", log.Err(err))
			return err
		}

		js, err = jetstream.New(nc)
		if err != nil {
			logger.ErrorContext(ctx, "failed to create JetStream context", log.Err(err))
			return err
		}

		return nil
	},
		retry.Context(ctx),
		retry.Attempts(10),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(n uint, err error) {
			logger.WarnContext(ctx, "retrying NATS connection", slog.Int("attempt", int(n)+1), log.Err(err))
		}),
	)

	return nc, js, err
}
