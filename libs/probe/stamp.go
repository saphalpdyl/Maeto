package probe

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/saphalpdyl/maeto/libs/stamp"
)

const (
	DefaultSTAMPPort uint16 = 862
)

var ErrReflectedUnsupported = errors.New("reflected two-way measurement is not supported: set no_reply")

func stampPort(cfg *ProbeConfigSTAMP) uint16 {
	if cfg.DestPort == 0 {
		return DefaultSTAMPPort
	}
	return cfg.DestPort
}

func stampConfig(cfg *ProbeConfigSTAMP) stamp.Config {
	stampCfg := stamp.Config{
		ErrorEstimate: stamp.ErrorEstimateConfig{
			Scale:        22,
			Multiplier:   1,
			Synchronized: false,
			ClockFormat:  stamp.ClockFormatNTP,
		},
	}

	if cfg.BindToDev != "" {
		stampCfg.BindToDev = &cfg.BindToDev
	}

	return stampCfg
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

func runSTAMPSender(ctx context.Context, cfg *ProbeConfigSTAMP, dispatcher Dispatcher, logger *slog.Logger) error {
	peer := cfg.PeerDestination.Addr()

	senderCfg := stamp.SenderConfig{
		LocalAddr:    ":0",
		RemoteAddr:   net.JoinHostPort(peer.String(), strconv.Itoa(int(stampPort(cfg)))),
		Config:       stampConfig(cfg),
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

		stampResults := STAMPResult{
			IsSender: true,
			Peer:     peer,
			Sequence: sender.Sequence(),
		}

		stampResultsJson, err := json.Marshal(stampResults)
		if err != nil {
			logger.WarnContext(ctx, "failed to marshal stamp results")
			continue
		}

		result := Result{
			ProbeType:    ProbeTypeSTAMP,
			TelemetryKey: cfg.TelemetryKey,
			SentAt:       time.Now(),
			Data:         stampResultsJson,
		}

		if err := sender.SendOnly(); err != nil {
			logger.WarnContext(ctx, "stamp probe failed",
				slog.String("peer", peer.String()),
				slog.Any("error", err),
			)

			err := sender.Close()
			if err != nil {
				logger.ErrorContext(ctx, "failed to close sender")
			}
			sender = nil

			continue
		}

		dispatch(ctx, dispatcher, result, logger)
	}
}

func runSTAMPReflector(ctx context.Context, cfg *ProbeConfigSTAMP, dispatcher Dispatcher, logger *slog.Logger) error {
	localAddr := net.JoinHostPort("", strconv.Itoa(int(stampPort(cfg))))

	controlChan := make(chan stamp.ProbePacket, 64)
	reflector, err := stamp.NewReflector(stamp.ReflectorConfig{
		LocalAddr: localAddr,
		HMACKey:   nil,
		OnError: func(err error) {
			logger.WarnContext(ctx, "reflector error", slog.Any("error", err))
		},
		Config: stampConfig(cfg),
	})

	if err != nil {
		return err
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- reflector.Serve(ctx, controlChan)
	}()

	for {
		select {
		case <-ctx.Done():
			err := reflector.Close()
			if err != nil && !errors.Is(err, net.ErrClosed) {
				return err
			}

			return ctx.Err()
		case err := <-serveErr:
			return err
		case pkt := <-controlChan:
			switch p := pkt.(type) {
			case *stamp.SenderPacket:
				// don't care about sender packets
				continue
			case *stamp.ReflectorPacket:
				result, err := reflectorResult(cfg, p)
				if err != nil {
					logger.WarnContext(ctx, "failed to build stamp reflector result",
						slog.Any("error", err),
					)
					continue
				}

				dispatch(ctx, dispatcher, result, logger)
			default:
				logger.WarnContext(ctx, "unexpected packet type", slog.Any("packet", pkt))
			}
		}
	}
}

func reflectorResult(cfg *ProbeConfigSTAMP, pkt *stamp.ReflectorPacket) (Result, error) {
	clockFormat := stampConfig(cfg).ErrorEstimate.ClockFormat

	senderTimestamp, err := pkt.SenderTimestamp.ToTime(clockFormat)
	if err != nil {
		return Result{}, err
	}

	receiveTimestamp, err := pkt.ReceiveTimestamp.ToTime(clockFormat)
	if err != nil {
		return Result{}, err
	}

	senderSequence := pkt.SenderSequenceNumber

	stampResult := STAMPResult{
		IsSender:         false,
		Sequence:         pkt.SequenceNumber,
		Peer:             cfg.PeerDestination.Addr(),
		SenderSequence:   &senderSequence,
		SenderTimestamp:  senderTimestamp,
		ReceiveTimestamp: receiveTimestamp,
	}

	stampResultJson, err := json.Marshal(stampResult)
	if err != nil {
		return Result{}, err
	}

	return Result{
		ProbeType:    ProbeTypeSTAMP,
		TelemetryKey: telemetryKey(cfg, pkt.SRExtensions),
		SentAt:       *senderTimestamp,
		Data:         stampResultJson,
	}, nil
}

func telemetryKey(cfg *ProbeConfigSTAMP, ext *stamp.SRExtensions) string {
	if ext == nil || ext.MaetoContainer == nil || ext.MaetoContainer.TelemetryKey == nil {
		return cfg.TelemetryKey
	}

	if ext.MaetoContainer.TelemetryKey.Key == "" {
		return cfg.TelemetryKey
	}

	return ext.MaetoContainer.TelemetryKey.Key
}

func dispatch(ctx context.Context, dispatcher Dispatcher, result Result, logger *slog.Logger) {
	if dispatcher == nil {
		return
	}

	if err := dispatcher.Dispatch(ctx, result); err != nil {
		logger.ErrorContext(ctx, "failed to dispatch probe result",
			slog.Any("error", err),
		)
	}
}
