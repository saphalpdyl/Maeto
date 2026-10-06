package controlplane

import (
	"context"
	"log/slog"
	"strings"
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

func newRegistry() *BandwidthReservationRegistry {
	r := NewBandwidthReservationRegistry(slog.New(slog.DiscardHandler))
	return &r
}

func res(source, sink NodeID, sourceMbps, sinkMbps float64) *BandwidthReservation {
	return labeled(source, sink, 0, sourceMbps, sinkMbps)
}

func labeled(source, sink NodeID, label int64, sourceMbps, sinkMbps float64) *BandwidthReservation {
	return &BandwidthReservation{
		Key:             BandwidthReservationKey{SourcePop: source, SinkPop: sink, Label: label},
		SourceBandwidth: sourceMbps,
		SinkBandwidth:   sinkMbps,
	}
}

func pceEdge(g *PCEGraph, name string) *PCEEdge {
	from, to, _ := strings.Cut(name, "->")
	for _, edge := range g.edges {
		if edge.NodeFrom == NodeID(from) && edge.NodeTo == NodeID(to) {
			return edge
		}
	}

	return nil
}

func idOf(g *PCEGraph, name string) PCEEdgeID {
	return pceEdge(g, name).ID
}

func ids(g *PCEGraph, names ...string) []PCEEdgeID {
	out := make([]PCEEdgeID, len(names))
	for i, name := range names {
		out[i] = idOf(g, name)
	}

	return out
}

func edgesOf(g *PCEGraph, names ...string) []*PCEEdge {
	out := make([]*PCEEdge, len(names))
	for i, name := range names {
		out[i] = pceEdge(g, name)
	}

	return out
}

func bookedOn(r *BandwidthReservationRegistry, g *PCEGraph, name string) float64 {
	rEdge, exists := r.registry[idOf(g, name)]
	if !exists {
		return 0
	}

	total := 0.0
	for _, entry := range rEdge.Bookings {
		total += entry.CalculateBandwidth()
	}

	return total
}

func freeOn(r *BandwidthReservationRegistry, g *PCEGraph, name string) float64 {
	return pceEdge(g, name).Capacity()*(1-capacityHeadroom) - bookedOn(r, g, name)
}

func Test_MoveAdmitsAgainstHeadroom(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()

	// 1000 Mbps link, 5% headroom, so 950 is bookable
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, res("A", "B", 600, 600)))
	assert.InDelta(t, 350, freeOn(r, g, "a->b"), 0.001)

	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, res("P", "Q", 350, 350)))
	assert.InDelta(t, 0, freeOn(r, g, "a->b"), 0.001)
}

func Test_MoveRejectsOverCapacity(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()

	require.Error(t, r.Move(context.Background(), nil, edgesOf(g, "b->c"), 1, res("A", "B", 96, 96)))
	assert.Empty(t, r.registry, "a rejected admission must not create an entry")
}

func Test_MoveIsAllOrNothing(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()

	// a-b can take it, b-c cannot. the whole batch has to be refused, and a-b
	// must not be left debited for a path that was never admitted
	require.Error(t, r.Move(context.Background(), nil, edgesOf(g, "a->b", "b->c"), 1, res("A", "B", 200, 200)))

	_, booked := r.registry[idOf(g, "a->b")]
	assert.False(t, booked, "a-b was debited for a batch that failed on b-c")
}

func Test_MoveReleasesTheOldPath(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()
	reservation := res("A", "B", 50, 50)

	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b", "b->c"), 1, reservation))

	// reroute onto the bypass: the old hops have to come back
	require.NoError(t, r.Move(ctx, ids(g, "a->b", "b->c"), edgesOf(g, "a->c"), 1, reservation))

	assert.InDelta(t, 950, freeOn(r, g, "a->b"), 0.001)
	assert.InDelta(t, 95, freeOn(r, g, "b->c"), 0.001)
	assert.InDelta(t, 900, freeOn(r, g, "a->c"), 0.001)

	assert.NotContains(t, r.registry, idOf(g, "a->b"), "emptied edge was left behind")
	assert.NotContains(t, r.registry, idOf(g, "b->c"), "emptied edge was left behind")
}

func Test_MoveRebookingTheSamePairIsNotDoubleCounted(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()
	reservation := res("A", "B", 950, 950)

	// book a-b to the brim, then "reroute" over a path that still uses it.
	// counted twice this would overdraw; replaced in place it is a no-op
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, reservation))
	require.NoError(t, r.Move(ctx, ids(g, "a->b"), edgesOf(g, "a->b"), 1, reservation))
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, reservation))

	assert.InDelta(t, 0, freeOn(r, g, "a->b"), 0.001)
}

func Test_MoveFreeingAnUnbookedEdgeIsANoop(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()

	require.NoError(t, r.Move(context.Background(), ids(g, "a->b"), nil, 1, res("A", "B", 100, 100)))
	assert.Empty(t, r.registry)
}

func Test_MoveReleasesAnEdgeThatLeftTheGraph(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()
	reservation := res("A", "B", 100, 100)

	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, reservation))
	gone := idOf(g, "a->b")

	// a-b goes down: the next graph has no edge for it, only its id survives
	down := reservationGraph()
	down.edges["a-b"].Up = false
	g = NewPCEGraph(down)
	require.Nil(t, pceEdge(g, "a->b"))

	require.NoError(t, r.Move(ctx, []PCEEdgeID{gone}, edgesOf(g, "a->c"), 1, reservation))
	assert.NotContains(t, r.registry, gone, "booking on a vanished edge leaked")
}

func Test_HoseCountsASharedSourceOnce(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()

	// A sends to B and C over the same edge. A can only send 600 in total
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, res("A", "B", 600, 300)))
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, res("A", "C", 600, 800)))

	assert.InDelta(t, 600, bookedOn(r, g, "a->b"), 0.001)
}

func Test_HoseCountsASharedSinkOnce(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()

	// A and P both send to B. B can only receive 300 in total
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, res("A", "B", 600, 300)))
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, res("P", "B", 400, 300)))

	assert.InDelta(t, 300, bookedOn(r, g, "a->b"), 0.001)
}

func Test_HoseAddsLabelsSeparately(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()

	// voice is source bound, bulk is sink bound. a single pooled min would
	// let one class borrow the other's slack and report 1100
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->c"), 1, labeled("A", "B", 1, 100, 1000)))
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->c"), 1, labeled("A", "B", 2, 1000, 100)))

	assert.InDelta(t, 200, bookedOn(r, g, "a->c"), 0.001)
}

func Test_MoveCountsOtherTenants(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()

	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 2, res("A", "B", 500, 500)))
	require.Error(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, res("A", "B", 500, 500)))

	assert.InDelta(t, 500, bookedOn(r, g, "a->b"), 0.001)
}

func Test_MoveKeepsOtherTenantsOutOfTheCandidatesBound(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()
	ctx := context.Background()

	// 400 + 400 fits in 950. folding tenant 2's reservation into tenant 1's
	// bound as well as counting it on its own would make it 1200
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 2, res("X", "Y", 400, 400)))
	require.NoError(t, r.Move(ctx, nil, edgesOf(g, "a->b"), 1, res("A", "B", 400, 400)))

	assert.InDelta(t, 800, bookedOn(r, g, "a->b"), 0.001)
}

func Test_PruneGraphDropsEdgesWithoutRoom(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()

	pruned, err := r.PruneGraph(g, 1, res("A", "B", 200, 200))
	require.NoError(t, err)

	// b-c is a 100 Mbps link, so it cannot hold a 200 Mbps reservation
	assert.NotContains(t, pruned.edges, idOf(g, "b->c"))
	assert.Contains(t, pruned.edges, idOf(g, "a->b"))
	assert.Contains(t, pruned.edges, idOf(g, "a->c"))

	assert.NotContains(t, pruned.adj["b"], idOf(g, "b->c"))
	assert.Contains(t, pruned.adj["a"], idOf(g, "a->b"))
}

func Test_PruneGraphLeavesTheOriginalAlone(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()

	_, err := r.PruneGraph(g, 1, res("A", "B", 200, 200))
	require.NoError(t, err)

	assert.Contains(t, g.edges, idOf(g, "b->c"), "prune mutated the caller's graph")
	assert.Contains(t, g.adj["b"], idOf(g, "b->c"))
}

func Test_PruneGraphReflectsBookedCapacity(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()

	// another tenant eats a-b down to 50 free, then ask for 200
	require.NoError(t, r.Move(context.Background(), nil, edgesOf(g, "a->b"), 2, res("X", "Y", 900, 900)))

	pruned, err := r.PruneGraph(g, 1, res("A", "B", 200, 200))
	require.NoError(t, err)

	assert.NotContains(t, pruned.edges, idOf(g, "a->b"), "prune ignored an existing reservation")
	assert.Contains(t, pruned.edges, idOf(g, "a->c"))
}

func Test_PruneGraphKeepsAnEdgeWhereTheHoseDoesNotGrow(t *testing.T) {
	g := NewPCEGraph(reservationGraph())
	r := newRegistry()

	// a-b holds A->B at 900, 50 free. A->C adds no new source and A caps the
	// bound at 900, so it fits even though 900 > 50
	require.NoError(t, r.Move(context.Background(), nil, edgesOf(g, "a->b"), 1, res("A", "B", 900, 900)))

	pruned, err := r.PruneGraph(g, 1, res("A", "C", 900, 900))
	require.NoError(t, err)

	assert.Contains(t, pruned.edges, idOf(g, "a->b"))
}
