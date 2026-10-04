package controlplane

import (
	"fmt"
	"slices"
	"sync"
)

type BandwidthReservationRegistry struct {
	mu       sync.RWMutex
	registry map[CanonicalEdgeID]map[TenantID]*BandwidthReservation
}

func NewBandwidthReservationRegistry() BandwidthReservationRegistry {
	return BandwidthReservationRegistry{
		mu:       sync.RWMutex{},
		registry: make(map[CanonicalEdgeID]map[TenantID]*BandwidthReservation),
	}
}

type BandwidthReservation struct {
	ForEdge CanonicalEdgeID
	Tenant  TenantID
	Booked  float64
	History []BandwidthReservationAction
}

type BandwidthReservationActionType string

const (
	BandwidthAdmit BandwidthReservationActionType = "bandwidth_admit"
)

type BandwidthReservationAction struct {
	ActionType BandwidthReservationActionType
	Tenant     TenantID
	FromPop    NodeID
	ToPop      NodeID

	Bandwidth float64
}

const capacityHeadroom = 0.05

func (b *BandwidthReservationRegistry) Move(from, to []*PCEEdge, action BandwidthReservationAction) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delta := make(map[CanonicalEdgeID]float64, len(from)+len(to))
	bookable := make(map[CanonicalEdgeID]float64, len(from)+len(to))

	for _, edge := range from {
		id := edge.CanonicalID()
		entry, booked := b.registry[id][action.Tenant]
		if !booked {
			continue
		}

		delta[id] -= min(action.Bandwidth, entry.Booked)
	}

	for _, edge := range to {
		id := edge.CanonicalID()
		delta[id] += action.Bandwidth

		capacity := edge.Bandwidth() * (1 - capacityHeadroom)
		if current, seen := bookable[id]; !seen || capacity < current {
			bookable[id] = capacity
		}
	}

	for id, change := range delta {
		if change <= 0 {
			continue
		}

		if free := bookable[id] - b.booked(id); change > free {
			return fmt.Errorf("edge %s holds %.2f, needs %.2f", id, free, change)
		}
	}

	for id, change := range delta {
		if change == 0 {
			continue
		}

		tenants, exists := b.registry[id]
		if !exists {
			tenants = make(map[TenantID]*BandwidthReservation)
			b.registry[id] = tenants
		}

		entry, booked := tenants[action.Tenant]
		if !booked {
			entry = &BandwidthReservation{ForEdge: id, Tenant: action.Tenant}
			tenants[action.Tenant] = entry
		}

		entry.Booked += change
		entry.History = append(entry.History, action)
	}

	return nil
}

func (b *BandwidthReservationRegistry) booked(id CanonicalEdgeID) float64 {
	total := 0.0
	for _, reservation := range b.registry[id] {
		total += reservation.Booked
	}

	return total
}

func (b *BandwidthReservationRegistry) CheckCapacity(edge *PCEEdge, bandwidth float64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	free := edge.Bandwidth()*(1-capacityHeadroom) - b.booked(edge.CanonicalID())
	return free-bandwidth >= 0
}

func (b *BandwidthReservationRegistry) PruneGraph(graph *PCEGraph, reserveBandwidth float64) (*PCEGraph, error) {
	pruned := &PCEGraph{
		nodes: graph.nodes, // read-only
		edges: make(map[string]*PCEEdge, len(graph.edges)),
		adj:   make(map[NodeID][]string, len(graph.adj)),
	}

	for id, edge := range graph.edges {
		if b.CheckCapacity(edge, reserveBandwidth) {
			pruned.edges[id] = edge
		}
	}

	for n, ids := range graph.adj {
		kept := slices.DeleteFunc(slices.Clone(ids), func(id string) bool {
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
	Edge     CanonicalEdgeID `json:"edge"`
	Bookable float64         `json:"bookable_mbps"`
	Booked   float64         `json:"booked_mbps"`
	Free     float64         `json:"free_mbps"`
	Bookings int             `json:"bookings"`
}

// Reports every edge in the graph: unbooked is fully free, not absent.
func (b *BandwidthReservationRegistry) Snapshot(graph *PCEGraph) map[CanonicalEdgeID]BandwidthReservationState {
	if b == nil || graph == nil {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	states := make(map[CanonicalEdgeID]BandwidthReservationState, len(graph.edges))
	for _, edge := range graph.edges {
		id := edge.CanonicalID()
		if _, seen := states[id]; seen {
			continue
		}

		bookable := edge.Bandwidth() * (1 - capacityHeadroom)
		booked := b.booked(id)

		bookings := 0
		for _, reservation := range b.registry[id] {
			bookings += len(reservation.History)
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
