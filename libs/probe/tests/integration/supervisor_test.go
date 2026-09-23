//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/saphalpdyl/maeto/libs/probe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const loopbackPort = 19863

type collector struct {
	results chan probe.Result
}

func (c *collector) Dispatch(ctx context.Context, result probe.Result) error {
	select {
	case c.results <- result:
	default:
	}
	return nil
}

func awaitResult(t *testing.T, sink *collector) probe.Result {
	t.Helper()

	select {
	case result := <-sink.results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("no probe result dispatched")
		return probe.Result{}
	}
}

func awaitSTAMPResult(t *testing.T, sink *collector, isSender bool) (probe.Result, probe.STAMPResult) {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for {
		select {
		case result := <-sink.results:
			var stampResult probe.STAMPResult
			require.NoError(t, json.Unmarshal(result.Data, &stampResult))

			if stampResult.IsSender != isSender {
				continue
			}

			return result, stampResult
		case <-deadline:
			t.Fatal("no probe result dispatched")
			return probe.Result{}, probe.STAMPResult{}
		}
	}
}

func stampProbe(sender bool, telemetryKey string) *probe.ProbeConfigSTAMP {
	return &probe.ProbeConfigSTAMP{
		ProbeType:       probe.ProbeTypeSTAMP,
		PeerDestination: netip.MustParsePrefix("127.0.0.1/32"),
		TelemetryKey:    telemetryKey,
		DestPort:        loopbackPort,
		IsSender:        sender,
		NoReply:         sender,
		ProbeInterval:   100 * time.Millisecond,
	}
}

func newSupervisor(t *testing.T, sink *collector) *probe.Supervisor {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sup := probe.NewSupervisor(probe.SupervisorConfig{
		Dispatcher:  sink,
		StopTimeout: 2 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)

	sup.SetBaseContextForTest(ctx)

	t.Cleanup(func() { _ = sup.Shutdown() })

	return sup
}

func Test_SupervisorSendsOneWayProbes(t *testing.T) {
	sink := &collector{results: make(chan probe.Result, 8)}
	sup := newSupervisor(t, sink)

	reflector := stampProbe(false, "tk-oneway")
	sender := stampProbe(true, "tk-oneway")

	require.NoError(t, sup.ReconcileWithTarget(map[string]probe.ProbeConfig{
		reflector.GetID(): reflector,
		sender.GetID():    sender,
	}))

	first, firstSTAMP := awaitSTAMPResult(t, sink, true)
	assert.Equal(t, "tk-oneway", first.TelemetryKey)
	assert.Equal(t, probe.ProbeTypeSTAMP, first.ProbeType)
	assert.Equal(t, uint32(0), firstSTAMP.Sequence)
	assert.Equal(t, netip.MustParseAddr("127.0.0.1"), firstSTAMP.Peer)
	assert.Nil(t, firstSTAMP.SenderSequence)
	assert.False(t, first.SentAt.IsZero())

	second, secondSTAMP := awaitSTAMPResult(t, sink, true)
	assert.Equal(t, uint32(1), secondSTAMP.Sequence)
	assert.True(t, second.SentAt.After(first.SentAt))

	require.NoError(t, sup.Shutdown())
	assert.Empty(t, sup.Running())
}

func Test_SupervisorStopsProbesOnReconcile(t *testing.T) {
	sink := &collector{results: make(chan probe.Result, 8)}
	sup := newSupervisor(t, sink)

	reflector := stampProbe(false, "tk-stop")
	sender := stampProbe(true, "tk-stop")

	require.NoError(t, sup.ReconcileWithTarget(map[string]probe.ProbeConfig{
		reflector.GetID(): reflector,
		sender.GetID():    sender,
	}))
	awaitResult(t, sink)

	require.NoError(t, sup.ReconcileWithTarget(map[string]probe.ProbeConfig{
		reflector.GetID(): reflector,
	}))

	time.Sleep(300 * time.Millisecond)

	for len(sink.results) > 0 {
		<-sink.results
	}

	time.Sleep(300 * time.Millisecond)
	assert.Empty(t, sink.results)
}

func Test_SupervisorReflectorReportsReceivedProbes(t *testing.T) {
	sink := &collector{results: make(chan probe.Result, 32)}
	sup := newSupervisor(t, sink)

	reflector := stampProbe(false, "tk-reflector-local")
	sender := stampProbe(true, "tk-on-wire")

	require.NoError(t, sup.ReconcileWithTarget(map[string]probe.ProbeConfig{
		reflector.GetID(): reflector,
		sender.GetID():    sender,
	}))

	result, stampResult := awaitSTAMPResult(t, sink, false)

	assert.Equal(t, probe.ProbeTypeSTAMP, result.ProbeType)
	assert.Equal(t, "tk-on-wire", result.TelemetryKey)
	assert.Equal(t, netip.MustParseAddr("127.0.0.1"), stampResult.Peer)

	require.NotNil(t, stampResult.SenderSequence)
	require.NotNil(t, stampResult.SenderTimestamp)
	require.NotNil(t, stampResult.ReceiveTimestamp)

	assert.True(t, result.SentAt.Equal(*stampResult.SenderTimestamp))
	assert.False(t, stampResult.ReceiveTimestamp.Before(*stampResult.SenderTimestamp))

	require.NoError(t, sup.Shutdown())
}
