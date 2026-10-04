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
	return BandwidthReservationAction{ActionType: BandwidthAdmit, Tenant: 1, Bandwidth: mbps}
}

func edgesOf(g *PCEGraph, ids ...string) []*PCEEdge {
	out := make([]*PCEEdge, len(ids))
	for i, id := range ids {
		out[i] = g.edges[id]
	}

	return out
}

func freeOn(t *testing.T, r *BandwidthReservationRegistry, g *PCEGraph, id string) float64 {
	t.Helper()

	edge := g.edges[id]
	_, ok := r.registry[edge.CanonicalID()]
	require.True(t, ok, "no reservation recorded for %s", id)

	return edge.Bandwidth()*(1-capacityHeadroom) - r.booked(edge.CanonicalID())
}

func Test_MoveAdmitsAgainstHeadroom(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	// 1000 Mbps link, 5% headroom, so 950 is bookable
	require.NoError(t, r.Move(nil, edgesOf(g, "a->b"), admit(600)))
	assert.InDelta(t, 350, freeOn(t, &r, g, "a->b"), 0.001)

	require.NoError(t, r.Move(nil, edgesOf(g, "a->b"), admit(350)))
	assert.InDelta(t, 0, freeOn(t, &r, g, "a->b"), 0.001)
}

func Test_MoveRejectsOverCapacity(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	require.Error(t, r.Move(nil, edgesOf(g, "b->c"), admit(96)))
	assert.Empty(t, r.registry, "a rejected admission must not create an entry")
}

func Test_MoveIsAllOrNothing(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	// a-b can take it, b-c cannot. the whole batch has to be refused, and a-b
	// must not be left debited for a path that was never admitted
	path := edgesOf(g, "a->b", "b->c")
	require.Error(t, r.Move(nil, path, admit(200)))

	_, booked := r.registry[g.edges["a->b"].CanonicalID()]
	assert.False(t, booked, "a-b was debited for a batch that failed on b-c")
}

func Test_MoveReleasesTheOldPath(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	old := edgesOf(g, "a->b", "b->c")
	require.NoError(t, r.Move(nil, old, admit(50)))

	// reroute onto the bypass: the old hops have to come back
	require.NoError(t, r.Move(old, edgesOf(g, "a->c"), admit(50)))

	assert.InDelta(t, 950, freeOn(t, &r, g, "a->b"), 0.001)
	assert.InDelta(t, 95, freeOn(t, &r, g, "b->c"), 0.001)
	assert.InDelta(t, 900, freeOn(t, &r, g, "a->c"), 0.001)
}

func Test_MoveNetsOutASharedHop(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	// book a-b to the brim, then "reroute" over a path that still uses it.
	// judged action by action this would overdraw; judged as a net change it
	// is a no-op
	shared := edgesOf(g, "a->b")
	require.NoError(t, r.Move(nil, shared, admit(950)))
	require.NoError(t, r.Move(shared, shared, admit(950)))

	assert.InDelta(t, 0, freeOn(t, &r, g, "a->b"), 0.001)
}

func Test_MoveFreeingAnUnbookedEdgeIsANoop(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	require.NoError(t, r.Move(edgesOf(g, "a->b"), nil, admit(100)))
	assert.Empty(t, r.registry)
}

func Test_PruneGraphDropsEdgesWithoutRoom(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	pruned, err := r.PruneGraph(g, 200)
	require.NoError(t, err)

	// b-c is a 100 Mbps link, so it cannot hold a 200 Mbps reservation
	assert.NotContains(t, pruned.edges, "b->c")
	assert.Contains(t, pruned.edges, "a->b")
	assert.Contains(t, pruned.edges, "a->c")

	assert.NotContains(t, pruned.adj["b"], "b->c")
	assert.NotContains(t, pruned.adj["c"], "b->c")
	assert.Contains(t, pruned.adj["a"], "a->b")
}

func Test_PruneGraphLeavesTheOriginalAlone(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	_, err := r.PruneGraph(g, 200)
	require.NoError(t, err)

	assert.Contains(t, g.edges, "b->c", "prune mutated the caller's graph")
	assert.Contains(t, g.adj["b"], "b->c")
}

func Test_PruneGraphReflectsBookedCapacity(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := NewBandwidthReservationRegistry()

	// eat a-b down to 50 free, then ask for 200
	require.NoError(t, r.Move(nil, edgesOf(g, "a->b"), admit(900)))

	pruned, err := r.PruneGraph(g, 200)
	require.NoError(t, err)

	assert.NotContains(t, pruned.edges, "a->b", "prune ignored an existing reservation")
	assert.Contains(t, pruned.edges, "a->c")
}
