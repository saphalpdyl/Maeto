package controlplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// a -- b -- c, plus a fat a -- c bypass, so pruning can change the answer
func reservationGraph() *Graph {
	edge := func(id EdgeID, from, to NodeID, mbps float64) *Edge {
		return &Edge{ID: id, Local: from, Remote: to, Bandwidth: mbps, Up: true}
	}

	return &Graph{
		nodes: map[NodeID]*Node{"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"}},
		edges: map[EdgeID]*Edge{
			"a-b": edge("a-b", "a", "b", 1000),
			"b-c": edge("b-c", "b", "c", 100),
			"a-c": edge("a-c", "a", "c", 1000),
		},
		adj: map[NodeID][]EdgeID{
			"a": {"a-b", "a-c"},
			"b": {"a-b", "b-c"},
			"c": {"b-c", "a-c"},
		},
		prefixes: map[string]*Prefix{},
	}
}

func admit(mbps float64) BandwidthReservationAction {
	return BandwidthReservationAction{ActionType: BandwidthAdmit, Bandwidth: mbps}
}

func capacityOf(t *testing.T, r *BandwidthReservationRegistry, id EdgeID) float64 {
	t.Helper()

	entry, ok := r.registry[id]
	require.True(t, ok, "no reservation recorded for %s", id)

	return entry.Capacity
}

func Test_MoveAdmitsAgainstHeadroom(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	// 1000 Mbps link, 5% headroom, so 950 is bookable
	require.NoError(t, r.Move(nil, []*Edge{g.edges["a-b"]}, admit(600)))
	assert.InDelta(t, 350, capacityOf(t, &r, "a-b"), 0.001)

	require.NoError(t, r.Move(nil, []*Edge{g.edges["a-b"]}, admit(350)))
	assert.InDelta(t, 0, capacityOf(t, &r, "a-b"), 0.001)
}

func Test_MoveRejectsOverCapacity(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	require.Error(t, r.Move(nil, []*Edge{g.edges["b-c"]}, admit(96)))
	assert.Empty(t, r.registry, "a rejected admission must not create an entry")
}

func Test_MoveIsAllOrNothing(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	// a-b can take it, b-c cannot. the whole batch has to be refused, and a-b
	// must not be left debited for a path that was never admitted
	path := []*Edge{g.edges["a-b"], g.edges["b-c"]}
	require.Error(t, r.Move(nil, path, admit(200)))

	_, booked := r.registry["a-b"]
	assert.False(t, booked, "a-b was debited for a batch that failed on b-c")
}

func Test_MoveReleasesTheOldPath(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	old := []*Edge{g.edges["a-b"], g.edges["b-c"]}
	require.NoError(t, r.Move(nil, old, admit(50)))

	// reroute onto the bypass: the old hops have to come back
	require.NoError(t, r.Move(old, []*Edge{g.edges["a-c"]}, admit(50)))

	assert.InDelta(t, 950, capacityOf(t, &r, "a-b"), 0.001)
	assert.InDelta(t, 95, capacityOf(t, &r, "b-c"), 0.001)
	assert.InDelta(t, 900, capacityOf(t, &r, "a-c"), 0.001)
}

func Test_MoveNetsOutASharedHop(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	// book a-b to the brim, then "reroute" over a path that still uses it.
	// judged action by action this would overdraw; judged as a net change it
	// is a no-op
	shared := []*Edge{g.edges["a-b"]}
	require.NoError(t, r.Move(nil, shared, admit(950)))
	require.NoError(t, r.Move(shared, shared, admit(950)))

	assert.InDelta(t, 0, capacityOf(t, &r, "a-b"), 0.001)
}

func Test_MoveFreeingAnUnbookedEdgeIsANoop(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	require.NoError(t, r.Move([]*Edge{g.edges["a-b"]}, nil, admit(100)))
	assert.Empty(t, r.registry)
}

func Test_PruneGraphDropsEdgesWithoutRoom(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	pruned, err := r.PruneGraph(g, 200)
	require.NoError(t, err)

	// b-c is a 100 Mbps link, so it cannot hold a 200 Mbps reservation
	assert.NotContains(t, pruned.edges, EdgeID("b-c"))
	assert.Contains(t, pruned.edges, EdgeID("a-b"))
	assert.Contains(t, pruned.edges, EdgeID("a-c"))

	assert.NotContains(t, pruned.adj["b"], EdgeID("b-c"))
	assert.NotContains(t, pruned.adj["c"], EdgeID("b-c"))
	assert.Contains(t, pruned.adj["a"], EdgeID("a-b"))
}

func Test_PruneGraphLeavesTheOriginalAlone(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	_, err := r.PruneGraph(g, 200)
	require.NoError(t, err)

	assert.Contains(t, g.edges, EdgeID("b-c"), "prune mutated the caller's graph")
	assert.Contains(t, g.adj["b"], EdgeID("b-c"))
}

func Test_PruneGraphReflectsBookedCapacity(t *testing.T) {
	g := reservationGraph()
	r := NewBandwidthReservationRegistry()

	// eat a-b down to 50 free, then ask for 200
	require.NoError(t, r.Move(nil, []*Edge{g.edges["a-b"]}, admit(900)))

	pruned, err := r.PruneGraph(g, 200)
	require.NoError(t, err)

	assert.NotContains(t, pruned.edges, EdgeID("a-b"), "prune ignored an existing reservation")
	assert.Contains(t, pruned.edges, EdgeID("a-c"))
}
