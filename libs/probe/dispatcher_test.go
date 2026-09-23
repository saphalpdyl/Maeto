package probe

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ JetStreamPublisher = (jetstream.JetStream)(nil)

type fakeJetStream struct {
	subjects []string
	payloads [][]byte
	streams  []jetstream.StreamConfig

	publishErr error
	streamErr  error
}

func (f *fakeJetStream) Publish(ctx context.Context, subject string, payload []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f.subjects = append(f.subjects, subject)
	f.payloads = append(f.payloads, payload)

	if f.publishErr != nil {
		return nil, f.publishErr
	}

	return &jetstream.PubAck{Stream: ProbeResultStream}, nil
}

func (f *fakeJetStream) CreateOrUpdateStream(_ context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	f.streams = append(f.streams, cfg)
	return nil, f.streamErr
}

func stampData(t *testing.T, sequence uint32) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(STAMPResult{
		IsSender: true,
		Sequence: sequence,
		Peer:     netip.MustParseAddr("fc00:0:1::1"),
	})
	require.NoError(t, err)

	return data
}

func probeResult(t *testing.T) Result {
	t.Helper()

	return Result{
		ProbeType:    ProbeTypeSTAMP,
		TelemetryKey: "A:eth1-B:eth3",
		SentAt:       time.Unix(1758326400, 0).UTC(),
		Data:         stampData(t, 7),
	}
}

func Test_NATSDispatcherAppliesDefaults(t *testing.T) {
	d := NewNATSDispatcher(&fakeJetStream{}, NATSDispatcherConfig{})

	assert.Equal(t, ProbeResultStream, d.cfg.StreamName)
	assert.Equal(t, ProbeResultSubjectPrefix, d.cfg.SubjectPrefix)
	assert.Equal(t, defaultResultMaxAge, d.cfg.MaxAge)
	assert.Equal(t, defaultPublishTimeout, d.cfg.PublishTimeout)
}

func Test_NATSDispatcherSubjectIsWildcardSafe(t *testing.T) {
	d := NewNATSDispatcher(&fakeJetStream{}, NATSDispatcherConfig{})

	assert.Equal(t, "maeto.probe.result.probe_stamp.a:eth1-b:eth3", d.Subject(probeResult(t)))

	unkeyed := probeResult(t)
	unkeyed.TelemetryKey = ""
	assert.Equal(t, "maeto.probe.result.probe_stamp.none", d.Subject(unkeyed))

	hostile := probeResult(t)
	hostile.TelemetryKey = "a.b c>d*e"
	assert.Equal(t, "maeto.probe.result.probe_stamp.a_b_c_d_e", d.Subject(hostile))
}

func Test_NATSDispatcherPublishesResult(t *testing.T) {
	js := &fakeJetStream{}
	d := NewNATSDispatcher(js, NATSDispatcherConfig{})

	require.NoError(t, d.Dispatch(context.Background(), probeResult(t)))

	require.Len(t, js.payloads, 1)
	assert.Equal(t, []string{"maeto.probe.result.probe_stamp.a:eth1-b:eth3"}, js.subjects)

	var decoded Result
	require.NoError(t, json.Unmarshal(js.payloads[0], &decoded))
	assert.Equal(t, probeResult(t), decoded)
}

func Test_NATSDispatcherMsgIDIsUniquePerPacket(t *testing.T) {
	first := probeResult(t)

	second := probeResult(t)
	second.Data = stampData(t, 8)

	assert.NotEqual(t, resultMsgID(first), resultMsgID(second))
	assert.Equal(t, resultMsgID(first), resultMsgID(probeResult(t)))
}

func Test_NATSDispatcherWrapsPublishFailure(t *testing.T) {
	sentinel := errors.New("no stream")
	d := NewNATSDispatcher(&fakeJetStream{publishErr: sentinel}, NATSDispatcherConfig{})

	err := d.Dispatch(context.Background(), probeResult(t))

	require.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), probeResult(t).TelemetryKey)
}

func Test_NATSDispatcherHonoursCancelledContext(t *testing.T) {
	js := &fakeJetStream{}
	d := NewNATSDispatcher(js, NATSDispatcherConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, d.Dispatch(ctx, probeResult(t)), context.Canceled)
	assert.Empty(t, js.payloads)
}

func Test_NATSDispatcherEnsureBindsSubjectTree(t *testing.T) {
	js := &fakeJetStream{}
	d := NewNATSDispatcher(js, NATSDispatcherConfig{MaxAge: time.Hour})

	require.NoError(t, d.Ensure(context.Background()))

	require.Len(t, js.streams, 1)
	assert.Equal(t, ProbeResultStream, js.streams[0].Name)
	assert.Equal(t, []string{"maeto.probe.result.>"}, js.streams[0].Subjects)
	assert.Equal(t, time.Hour, js.streams[0].MaxAge)
	assert.Equal(t, jetstream.FileStorage, js.streams[0].Storage)
}

func Test_NATSDispatcherEnsureWrapsFailure(t *testing.T) {
	sentinel := errors.New("jetstream down")
	d := NewNATSDispatcher(&fakeJetStream{streamErr: sentinel}, NATSDispatcherConfig{})

	require.ErrorIs(t, d.Ensure(context.Background()), sentinel)
}

func Test_NATSDispatcherSatisfiesDispatcher(t *testing.T) {
	var d Dispatcher = NewNATSDispatcher(&fakeJetStream{}, NATSDispatcherConfig{})
	require.NoError(t, d.Dispatch(context.Background(), probeResult(t)))
}
