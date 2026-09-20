package probe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type ProbeType string

const (
	ProbeTypeSTAMP ProbeType = "PROBE_STAMP"
)

const defaultStopTimeout = 10 * time.Second

type ProbeConfig interface {
	GetID() string
}

type ProbeRunner func(ctx context.Context, cfg ProbeConfig) error

type SupervisorConfig struct {
	Dispatcher  Dispatcher
	StopTimeout time.Duration
	Runner      ProbeRunner
}

type Supervisor struct {
	reconcileMu sync.Mutex

	mu      sync.Mutex
	logger  *slog.Logger
	baseCtx context.Context

	probes map[string]*Probe

	runner      ProbeRunner
	stopTimeout time.Duration

	generation uint64
	closed     bool
}

type Probe struct {
	CtxCancel  context.CancelFunc
	Config     ProbeConfig
	Generation uint64

	done chan struct{}
	err  error
}

func (p *Probe) Exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Probe) Err() error {
	select {
	case <-p.done:
		return p.err
	default:
		return nil
	}
}

func NewSupervisor(ctx context.Context, cfg SupervisorConfig, logger *slog.Logger) *Supervisor {
	s := &Supervisor{
		probes:      make(map[string]*Probe),
		generation:  0,
		logger:      logger,
		baseCtx:     ctx,
		runner:      cfg.Runner,
		stopTimeout: cfg.StopTimeout,
	}

	if s.runner == nil {
		s.runner = newDefaultRunner(cfg.Dispatcher, logger)
	}

	if s.stopTimeout <= 0 {
		s.stopTimeout = defaultStopTimeout
	}

	return s
}

func (s *Supervisor) ReconcileWithTarget(target map[string]ProbeConfig) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	if err := s.baseCtx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("supervisor is shut down")
	}

	s.generation++
	generation := s.generation

	stopped := make(map[string]*Probe)
	for id, probe := range s.probes {
		desired, keep := target[id]
		if keep && desired != nil && !probe.Exited() && probe.Config.GetID() == desired.GetID() {
			continue
		}

		delete(s.probes, id)
		stopped[id] = probe
	}
	s.mu.Unlock()

	s.stop(stopped, generation)

	var errs []error

	s.mu.Lock()
	for id, cfg := range target {
		if _, running := s.probes[id]; running {
			continue
		}

		if cfg == nil {
			errs = append(errs, fmt.Errorf("probe %q: nil config", id))
			continue
		}

		s.probes[id] = s.start(id, cfg, generation)
	}
	s.mu.Unlock()

	return errors.Join(errs...)
}

func (s *Supervisor) Shutdown() error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}

	s.closed = true
	s.generation++

	stopped := s.probes
	s.probes = make(map[string]*Probe)
	generation := s.generation
	s.mu.Unlock()

	s.stop(stopped, generation)

	var errs []error
	for id, probe := range stopped {
		if err := probe.Err(); err != nil {
			errs = append(errs, fmt.Errorf("probe %q: %w", id, err))
		}
	}

	return errors.Join(errs...)
}

func (s *Supervisor) Running() map[string]ProbeConfig {
	s.mu.Lock()
	defer s.mu.Unlock()

	running := make(map[string]ProbeConfig, len(s.probes))
	for id, probe := range s.probes {
		running[id] = probe.Config
	}

	return running
}

func (s *Supervisor) start(id string, cfg ProbeConfig, generation uint64) *Probe {
	probeCtx, cancel := context.WithCancel(s.baseCtx)

	probe := &Probe{
		CtxCancel:  cancel,
		Config:     cfg,
		Generation: generation,
		done:       make(chan struct{}),
	}

	s.logger.InfoContext(s.baseCtx, "starting probe",
		slog.String("probe", id),
		slog.Uint64("generation", generation),
	)

	go func() {
		defer close(probe.done)
		defer cancel()

		err := s.runner(probeCtx, cfg)
		if err != nil && !errors.Is(err, context.Canceled) {
			probe.err = err
			s.logger.ErrorContext(probeCtx, "probe exited with error",
				slog.String("probe", id),
				slog.Any("error", err),
			)
			return
		}

		s.logger.InfoContext(probeCtx, "probe stopped", slog.String("probe", id))
	}()

	return probe
}

func (s *Supervisor) stop(stopped map[string]*Probe, generation uint64) {
	if len(stopped) == 0 {
		return
	}

	for id, probe := range stopped {
		s.logger.InfoContext(s.baseCtx, "stopping probe",
			slog.String("probe", id),
			slog.Uint64("generation", generation),
		)
		probe.CtxCancel()
	}

	deadline := time.NewTimer(s.stopTimeout)
	defer deadline.Stop()

	for id, probe := range stopped {
		select {
		case <-probe.done:
		case <-deadline.C:
			s.logger.WarnContext(s.baseCtx, "timed out waiting for probe to stop",
				slog.String("probe", id),
				slog.Duration("timeout", s.stopTimeout),
			)
			return
		}
	}
}
