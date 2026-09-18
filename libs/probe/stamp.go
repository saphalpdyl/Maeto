package probe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/saphalpdyl/maeto/libs/stamp"
)

const (
	DefaultSTAMPPort uint16 = 862

	restartBackoff = 5 * time.Second
)

var ErrReflectedUnsupported = errors.New("reflected two-way measurement is not supported: set no_reply")

func newDefaultRunner(supCfg SupervisorConfig, logger *slog.Logger) ProbeRunner {
	return func(ctx context.Context, cfg ProbeConfig) error {
		switch cfg := cfg.(type) {
		case *ProbeConfigSTAMP:
			if err := cfg.Validate(); err != nil {
				return err
			}

			if cfg.IsSender {
				return runSTAMPSender(ctx, cfg, supCfg, logger)
			}
			return runSTAMPReflector(ctx, cfg, supCfg, logger)
		default:
			return fmt.Errorf("unsupported probe config %T", cfg)
		}
	}
}

func stampPort(cfg *ProbeConfigSTAMP) uint16 {
	if cfg.Port == 0 {
		return DefaultSTAMPPort
	}
	return cfg.Port
}

func stampConfig(bindToDev *string) stamp.Config {
	return stamp.Config{
		ErrorEstimate: stamp.ErrorEstimateConfig{
			Scale:        22,
			Multiplier:   1,
			Synchronized: false,
			ClockFormat:  stamp.ClockFormatNTP,
		},
		BindToDev: bindToDev,
	}
}

func srExtensions(cfg *ProbeConfigSTAMP) *stamp.SRExtensions {
	if cfg.TelemetryKey == "" {
		return nil
	}

	return &stamp.SRExtensions{
		MaetoContainer: &stamp.SRExtMaetoContainer{
			TelemetryKey: &stamp.SRExtMaetoContainerTelemetryKey{Key: cfg.TelemetryKey},
		},
	}
}

func runSTAMPSender(ctx context.Context, cfg *ProbeConfigSTAMP, supCfg SupervisorConfig, logger *slog.Logger) error {
	peer := cfg.PeerDestination.Addr()

	senderCfg := stamp.SenderConfig{
		LocalAddr:    ":0",
		RemoteAddr:   net.JoinHostPort(peer.String(), strconv.Itoa(int(stampPort(cfg)))),
		Config:       stampConfig(supCfg.BindToDev),
		SRExtensions: srExtensions(cfg),
		OnError: func(err error) {
			logger.WarnContext(ctx, "stamp sender error", slog.Any("error", err))
		},
	}

	var sender *stamp.Sender

	defer func() {
		if sender != nil {
			err := sender.Close()
			if err != nil {
				logger.ErrorContext(ctx, "failed to close sender")
			}
		}
	}()

	ticker := time.NewTicker(cfg.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		if sender == nil {
			opened, err := stamp.NewSender(senderCfg)
			if err != nil {
				logger.WarnContext(ctx, "failed to open stamp sender",
					slog.String("peer", peer.String()),
					slog.Any("error", err),
				)
				continue
			}
			sender = opened
		}

		result := Result{
			ProbeID:      cfg.GetID(),
			ProbeType:    ProbeTypeSTAMP,
			TelemetryKey: cfg.TelemetryKey,
			Peer:         peer,
			Sequence:     sender.Sequence(),
			SentAt:       time.Now(),
		}

		if err := sender.SendOnly(); err != nil {
			logger.WarnContext(ctx, "stamp probe failed",
				slog.String("peer", peer.String()),
				slog.Uint64("sequence", uint64(result.Sequence)),
				slog.Any("error", err),
			)

			err := sender.Close()
			if err != nil {
				logger.ErrorContext(ctx, "failed to close sender")
			}
			sender = nil

			continue
		}

		dispatch(ctx, supCfg.Dispatcher, result, logger)
	}
}

func runSTAMPReflector(ctx context.Context, cfg *ProbeConfigSTAMP, supCfg SupervisorConfig, logger *slog.Logger) error {
	localAddr := net.JoinHostPort("", strconv.Itoa(int(stampPort(cfg))))

	for {
		reflector, err := stamp.NewReflector(stamp.ReflectorConfig{
			LocalAddr: localAddr,
			Config:    stampConfig(supCfg.BindToDev),
			OnError: func(err error) {
				logger.WarnContext(ctx, "stamp reflector error", slog.Any("error", err))
			},
		})
		if err == nil {
			err = reflector.Serve(ctx)
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			logger.WarnContext(ctx, "stamp reflector exited, restarting",
				slog.String("local_addr", localAddr),
				slog.Duration("backoff", restartBackoff),
				slog.Any("error", err),
			)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(restartBackoff):
		}
	}
}

func dispatch(ctx context.Context, dispatcher Dispatcher, result Result, logger *slog.Logger) {
	if dispatcher == nil {
		return
	}

	if err := dispatcher.Dispatch(ctx, result); err != nil {
		logger.ErrorContext(ctx, "failed to dispatch probe result",
			slog.String("probe", result.ProbeID),
			slog.Any("error", err),
		)
	}
}
