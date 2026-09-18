package probe_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeConfig struct {
	id string
}

func (f fakeConfig) GetID() string { return f.id }

type harness struct {
	mu      sync.Mutex
	events  []string
	starts  map[string]int
	exitNow map[string]error
}

func newHarness() *harness {
	return &harness{
		starts:  make(map[string]int),
		exitNow: make(map[string]error),
	}
}

func (h *harness) runner(ctx context.Context, cfg probe.ProbeConfig) error {
	id := cfg.GetID()

	h.mu.Lock()
	h.starts[id]++
	h.events = append(h.events, "start:"+id)
	err, exit := h.exitNow[id]
	h.mu.Unlock()

	if exit {
		return err
	}

	<-ctx.Done()

	h.mu.Lock()
	h.events = append(h.events, "stop:"+id)
	h.mu.Unlock()

	return ctx.Err()
}

func (h *harness) startCount(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.starts[id]
}

func (h *harness) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.events)
}

func newTestSupervisor(t *testing.T, h *harness) *probe.Supervisor {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sup := probe.NewSupervisor(ctx, probe.SupervisorConfig{
		Runner:      h.runner,
		StopTimeout: 2 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Cleanup(func() { _ = sup.Shutdown() })

	return sup
}

func target(ids ...string) map[string]probe.ProbeConfig {
	out := make(map[string]probe.ProbeConfig, len(ids))
	for _, id := range ids {
		out[id] = fakeConfig{id: id}
	}
	return out
}

func Test_ReconcileTracksStartedProbes(t *testing.T) {
	h := newHarness()
	sup := newTestSupervisor(t, h)

	require.NoError(t, sup.ReconcileWithTarget(target("a", "b")))
	assert.Equal(t, target("a", "b"), sup.Running())

	require.Eventually(t, func() bool {
		return h.startCount("a") == 1 && h.startCount("b") == 1
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, sup.ReconcileWithTarget(target("a", "b")))
	assert.Equal(t, 1, h.startCount("a"))
	assert.Equal(t, 1, h.startCount("b"))
}

func Test_ReconcileStopsRemovedProbes(t *testing.T) {
	h := newHarness()
	sup := newTestSupervisor(t, h)

	require.NoError(t, sup.ReconcileWithTarget(target("a", "b")))
	require.NoError(t, sup.ReconcileWithTarget(target("a")))

	assert.Equal(t, target("a"), sup.Running())
	assert.Contains(t, h.snapshot(), "stop:b")

	require.NoError(t, sup.ReconcileWithTarget(nil))
	assert.Empty(t, sup.Running())
	assert.Contains(t, h.snapshot(), "stop:a")
}

func Test_ReconcileRestartsOnConfigChange(t *testing.T) {
	h := newHarness()
	sup := newTestSupervisor(t, h)

	require.NoError(t, sup.ReconcileWithTarget(map[string]probe.ProbeConfig{
		"peer-1": fakeConfig{id: "v1"},
	}))
	require.NoError(t, sup.ReconcileWithTarget(map[string]probe.ProbeConfig{
		"peer-1": fakeConfig{id: "v2"},
	}))

	require.Eventually(t, func() bool {
		return h.startCount("v2") == 1
	}, time.Second, 10*time.Millisecond)

	events := h.snapshot()
	assert.Less(t, slices.Index(events, "stop:v1"), slices.Index(events, "start:v2"))
}

func Test_ReconcileRestartsExitedProbe(t *testing.T) {
	h := newHarness()
	h.exitNow["crashed"] = errors.New("boom")

	sup := newTestSupervisor(t, h)

	require.Eventually(t, func() bool {
		require.NoError(t, sup.ReconcileWithTarget(target("crashed")))
		return h.startCount("crashed") == 2
	}, time.Second, 10*time.Millisecond)
}

func Test_ReconcileRejectsNilConfig(t *testing.T) {
	h := newHarness()
	sup := newTestSupervisor(t, h)

	err := sup.ReconcileWithTarget(map[string]probe.ProbeConfig{"a": nil})
	require.Error(t, err)
	assert.Empty(t, sup.Running())
}

func Test_ReconcileStopsProbeReplacedByNilConfig(t *testing.T) {
	h := newHarness()
	sup := newTestSupervisor(t, h)

	require.NoError(t, sup.ReconcileWithTarget(target("a")))
	require.Error(t, sup.ReconcileWithTarget(map[string]probe.ProbeConfig{"a": nil}))

	assert.Empty(t, sup.Running())
	assert.Contains(t, h.snapshot(), "stop:a")
}

func Test_ShutdownStopsEverything(t *testing.T) {
	h := newHarness()
	sup := newTestSupervisor(t, h)

	require.NoError(t, sup.ReconcileWithTarget(target("a", "b")))
	require.NoError(t, sup.Shutdown())

	assert.Empty(t, sup.Running())
	assert.Contains(t, h.snapshot(), "stop:a")
	assert.Contains(t, h.snapshot(), "stop:b")

	assert.Error(t, sup.ReconcileWithTarget(target("a")))
	assert.NoError(t, sup.Shutdown())
}

func Test_ReconcileFailsOnCancelledBaseContext(t *testing.T) {
	h := newHarness()

	ctx, cancel := context.WithCancel(context.Background())
	sup := probe.NewSupervisor(ctx, probe.SupervisorConfig{
		Runner: h.runner,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	cancel()

	assert.ErrorIs(t, sup.ReconcileWithTarget(target("a")), context.Canceled)
	assert.Empty(t, sup.Running())
}

func stampConfig() *probe.ProbeConfigSTAMP {
	return &probe.ProbeConfigSTAMP{
		ProbeType:       probe.ProbeTypeSTAMP,
		PeerDestination: netip.MustParsePrefix("2001:db8::1/128"),
		TelemetryKey:    "key",
		IsSender:        true,
		NoReply:         true,
		ProbeInterval:   time.Second,
	}
}

func Test_STAMPConfigIDDistinguishesRoles(t *testing.T) {
	sender := stampConfig()

	reflector := *sender
	reflector.IsSender = false

	reflected := *sender
	reflected.NoReply = false

	assert.NotEqual(t, sender.GetID(), reflector.GetID())
	assert.NotEqual(t, sender.GetID(), reflected.GetID())
}

func Test_STAMPConfigRejectsReflectedMeasurement(t *testing.T) {
	cfg := stampConfig()
	cfg.NoReply = false

	assert.ErrorIs(t, cfg.Validate(), probe.ErrReflectedUnsupported)
}

func Test_STAMPConfigValidate(t *testing.T) {
	assert.NoError(t, stampConfig().Validate())

	reflector := stampConfig()
	reflector.IsSender = false
	reflector.ProbeInterval = 0
	assert.NoError(t, reflector.Validate())

	noInterval := stampConfig()
	noInterval.ProbeInterval = 0
	assert.Error(t, noInterval.Validate())

	noPeer := stampConfig()
	noPeer.PeerDestination = netip.Prefix{}
	assert.Error(t, noPeer.Validate())
}
