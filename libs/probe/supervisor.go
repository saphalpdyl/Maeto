package probe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/saphalpdyl/maeto/libs/nodesync"
)

type ProbeType string

const (
	ProbeTypeSTAMP ProbeType = "PROBE_STAMP"
)

const defaultStopTimeout = 10 * time.Second

var errNotStarted = errors.New("supervisor has not been started")

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

	intentFeed <-chan *nodesync.NodeIntent
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

func NewSupervisor(
	cfg SupervisorConfig,
	logger *slog.Logger,
	intentFeed <-chan *nodesync.NodeIntent,
) *Supervisor {
	s := &Supervisor{
		probes:      make(map[string]*Probe),
		generation:  0,
		logger:      logger,
		baseCtx:     nil,
		runner:      cfg.Runner,
		stopTimeout: cfg.StopTimeout,
		intentFeed:  intentFeed,
	}

	if s.runner == nil {
		s.runner = NewDefaultRunner(cfg.Dispatcher, logger)
	}

	if s.stopTimeout <= 0 {
		s.stopTimeout = defaultStopTimeout
	}

	return s
}

func (s *Supervisor) setBaseContext(ctx context.Context) {
	s.mu.Lock()
	s.baseCtx = ctx
	s.mu.Unlock()
}

func (s *Supervisor) base() (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.baseCtx == nil {
		return nil, errNotStarted
	}

	return s.baseCtx, nil
}

func (s *Supervisor) Start(ctx context.Context) error {
	s.setBaseContext(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case nodeIntent := <-s.intentFeed:
			switch intent := nodeIntent.Intent.(type) {
			case *nodesync.CPEIntent:
				panic("cpe intent parsing is not supported")
			case *nodesync.PEIntent:
				if len(intent.Peers) > 0 {
					s.logger.DebugContext(ctx, "got peer intent ", slog.Any("intent", intent.Peers))
				}
			default:
				s.logger.ErrorContext(ctx, "unrecognized intent type")
			}
		}
	}
}

func (s *Supervisor) reconcileWithTarget(target map[string]ProbeConfig) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	baseCtx, err := s.base()
	if err != nil {
		return err
	}

	if err := baseCtx.Err(); err != nil {
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

	s.stop(baseCtx, stopped, generation)

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

		s.probes[id] = s.startProbe(baseCtx, id, cfg, generation)
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

	baseCtx := s.baseCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	s.mu.Unlock()

	s.stop(baseCtx, stopped, generation)

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

func (s *Supervisor) startProbe(baseCtx context.Context, id string, cfg ProbeConfig, generation uint64) *Probe {
	probeCtx, cancel := context.WithCancel(baseCtx)

	probe := &Probe{
		CtxCancel:  cancel,
		Config:     cfg,
		Generation: generation,
		done:       make(chan struct{}),
	}

	s.logger.InfoContext(baseCtx, "starting probe",
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

func (s *Supervisor) stop(baseCtx context.Context, stopped map[string]*Probe, generation uint64) {
	if len(stopped) == 0 {
		return
	}

	for id, probe := range stopped {
		s.logger.InfoContext(baseCtx, "stopping probe",
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
			s.logger.WarnContext(baseCtx, "timed out waiting for probe to stop",
				slog.String("probe", id),
				slog.Duration("timeout", s.stopTimeout),
			)
			return
		}
	}
}

func NewDefaultRunner(dispatcher Dispatcher, logger *slog.Logger) ProbeRunner {
	return func(ctx context.Context, cfg ProbeConfig) error {
		switch cfg := cfg.(type) {
		case *ProbeConfigSTAMP:
			if err := cfg.Validate(); err != nil {
				return err
			}

			if cfg.IsSender {
				return runSTAMPSender(ctx, cfg, dispatcher, logger)
			}
			return runSTAMPReflector(ctx, cfg, logger)
		default:
			return fmt.Errorf("unsupported probe config %T", cfg)
		}
	}
}
