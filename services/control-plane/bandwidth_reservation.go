package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"
)

// Notes and thoughts for the future me:
//   Sources and Sinks cannot be attributed to a POP
//		There will be multiple paths between two pops: voice, bulk etc. colored paths
// 		Make it so that its suffixed by another element

type BandwidthReservationRegistry struct {
	mu       sync.RWMutex
	registry map[PCEEdgeID]*BandwidthReservationEdge
	logger   *slog.Logger
}

func NewBandwidthReservationRegistry(logger *slog.Logger) BandwidthReservationRegistry {
	return BandwidthReservationRegistry{
		mu:       sync.RWMutex{},
		registry: make(map[PCEEdgeID]*BandwidthReservationEdge),
		logger:   logger,
	}
}

// Directional, one physical link will have two reservation edge
type BandwidthReservationEdge struct {
	ForEdge *PCEEdge

	Bookings map[TenantID]*BandwidthReservationTenantEntry
}

type BandwidthReservationTenantEntry struct {
	Tenant TenantID

	Reservations map[BandwidthReservationKey]*BandwidthReservation
}

type bandwidthPopLabel struct {
	Pop   NodeID
	Label int64
}

func (e *BandwidthReservationTenantEntry) getBandwidthSums() (map[int64]float64, map[int64]float64) {
	sumSources := make(map[int64]float64)
	sumSinks := make(map[int64]float64)

	seenSources := make(map[bandwidthPopLabel]struct{})
	seenSinks := make(map[bandwidthPopLabel]struct{})

	for _, r := range e.Reservations {
		source := bandwidthPopLabel{Pop: r.Key.SourcePop, Label: r.Key.Label}
		if _, exists := seenSources[source]; !exists {
			seenSources[source] = struct{}{}
			sumSources[r.Key.Label] += r.SourceBandwidth
		}

		sink := bandwidthPopLabel{Pop: r.Key.SinkPop, Label: r.Key.Label}
		if _, exists := seenSinks[sink]; !exists {
			seenSinks[sink] = struct{}{}
			sumSinks[r.Key.Label] += r.SinkBandwidth
		}
	}

	return sumSources, sumSinks
}

func (e *BandwidthReservationTenantEntry) CalculateBandwidth() float64 {
	sumSources, sumSinks := e.getBandwidthSums()

	total := 0.0
	for label := range sumSources {
		total += math.Min(sumSources[label], sumSinks[label])
	}

	return total
}

type BandwidthReservationKey struct {
	SourcePop NodeID
	SinkPop   NodeID
	Label     int64 // identifies color, tiers etc.
}

type BandwidthReservation struct {
	Key BandwidthReservationKey

	// Assume per-pop bandwidth consolidation (summing bandwidth of sites of same tenant) has
	// 	already been done by this point
	SourceBandwidth float64
	SinkBandwidth   float64
}

const capacityHeadroom = 0.05

func (b *BandwidthReservationRegistry) Move(
	ctx context.Context,
	from []PCEEdgeID,
	to []*PCEEdge,
	tenantID TenantID,
	candidate *BandwidthReservation,
) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, edge := range to {
		if !b.checkCapacity(edge, tenantID, candidate) {
			return fmt.Errorf("one of the edges cannot support capacity, edge: %s", edge.ID)
		}
	}

	// Remove reservations
	for _, edgeID := range from {
		if rEdge, exists := b.registry[edgeID]; exists {
			if tenantEntry, exists := rEdge.Bookings[tenantID]; exists {
				delete(tenantEntry.Reservations, candidate.Key)

				if len(tenantEntry.Reservations) == 0 {
					delete(rEdge.Bookings, tenantID)
				}
			}

			if len(rEdge.Bookings) == 0 {
				delete(b.registry, edgeID)
			}
		}
	}

	// Add reservations
	for _, edge := range to {
		rEdge, exists := b.registry[edge.ID]
		if !exists {
			b.registry[edge.ID] = &BandwidthReservationEdge{
				ForEdge:  edge,
				Bookings: make(map[TenantID]*BandwidthReservationTenantEntry),
			}
			rEdge = b.registry[edge.ID]
		}

		tenantEntry, exists := rEdge.Bookings[tenantID]
		if !exists {
			rEdge.Bookings[tenantID] = &BandwidthReservationTenantEntry{
				Tenant:       tenantID,
				Reservations: make(map[BandwidthReservationKey]*BandwidthReservation),
			}
			tenantEntry = rEdge.Bookings[tenantID]
		}

		r := *candidate
		tenantEntry.Reservations[candidate.Key] = &r
	}

	return nil
}

func (b *BandwidthReservationRegistry) checkCapacity(
	edge *PCEEdge,
	tenantID TenantID,
	candidate *BandwidthReservation,
) bool {
	var booked float64

	// Create a testReservation and add the source/sink to check
	// CalculateBandwidth() automatically handles the deduplication
	// 	Otherwise, we'd have to manage the oncoming candidate's source/sink pop dedupe
	//	ourselves/
	testTenantReservation := &BandwidthReservationTenantEntry{
		Tenant: tenantID,
		Reservations: map[BandwidthReservationKey]*BandwidthReservation{
			candidate.Key: candidate,
		},
	}

	// If !ok, then edge is empty, test directly if math.Min(source,sink) <= capacity
	if reservationEdge, ok := b.registry[edge.ID]; ok {
		for tID, entry := range reservationEdge.Bookings {
			if tID != tenantID {
				booked += entry.CalculateBandwidth()
				continue
			}

			for key, r := range entry.Reservations {
				if key != candidate.Key {
					testTenantReservation.Reservations[key] = r
				}
			}
		}
	}

	return booked+testTenantReservation.CalculateBandwidth() <= edge.Capacity()*(1-capacityHeadroom)
}

func (b *BandwidthReservationRegistry) CheckCapacity(
	edge *PCEEdge,
	tenantID TenantID,
	candidate *BandwidthReservation,
) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.checkCapacity(edge, tenantID, candidate)
}

func (b *BandwidthReservationRegistry) PruneGraph(graph *PCEGraph, tenantID TenantID, candidate *BandwidthReservation) (*PCEGraph, error) {
	pruned := &PCEGraph{
		nodes: graph.nodes, // read-only
		edges: make(map[PCEEdgeID]*PCEEdge, len(graph.edges)),
		adj:   make(map[NodeID][]PCEEdgeID, len(graph.adj)),
	}

	for id, edge := range graph.edges {
		if b.CheckCapacity(edge, tenantID, candidate) {
			pruned.edges[id] = edge
		}
	}

	for n, ids := range graph.adj {
		kept := slices.DeleteFunc(slices.Clone(ids), func(id PCEEdgeID) bool {
			_, ok := pruned.edges[id]
			return !ok
		})
		if len(kept) > 0 {
			pruned.adj[n] = kept
		}
	}
	return pruned, nil
}

type BandwidthReservationState struct {
	Edge     PCEEdgeID `json:"edge"`
	Bookable float64   `json:"bookable_mbps"`
	Booked   float64   `json:"booked_mbps"`
	Free     float64   `json:"free_mbps"`
	Bookings int       `json:"bookings"`
}

// Reports every edge in the graph: unbooked is fully free, not absent.
func (b *BandwidthReservationRegistry) Snapshot(graph *PCEGraph) map[PCEEdgeID]BandwidthReservationState {
	if b == nil || graph == nil {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	states := make(map[PCEEdgeID]BandwidthReservationState, len(graph.edges))
	for id, edge := range graph.edges {
		bookable := edge.Capacity() * (1 - capacityHeadroom)

		booked := 0.0
		bookings := 0
		if rEdge, exists := b.registry[id]; exists {
			for _, tenantEntry := range rEdge.Bookings {
				booked += tenantEntry.CalculateBandwidth()
				bookings += len(tenantEntry.Reservations)
			}
		}

		states[id] = BandwidthReservationState{
			Edge:     id,
			Bookable: bookable,
			Booked:   booked,
			Free:     bookable - booked,
			Bookings: bookings,
		}
	}

	return states
}
