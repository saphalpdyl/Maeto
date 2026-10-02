package controlplane

import (
	"fmt"
	"slices"
	"sync"
)

type BandwidthReservationRegistry struct {
	mu       sync.RWMutex
	registry map[EdgeID]*BandwidthReservation
}

func NewBandwidthReservationRegistry() BandwidthReservationRegistry {
	return BandwidthReservationRegistry{
		mu:       sync.RWMutex{},
		registry: make(map[EdgeID]*BandwidthReservation),
	}
}

type BandwidthReservation struct {
	ForEdge  EdgeID
	FromNode NodeID
	ToNode   NodeID

	CapacityHeadroomPercentage float64
	Capacity                   float64
	Residual                   float64
	History                    []BandwidthReservationAction
}

type BandwidthReservationActionType string

const (
	BandwidthAdmit BandwidthReservationActionType = "bandwidth_admit"
)

type BandwidthReservationAction struct {
	ActionType     BandwidthReservationActionType
	TenantSiteFrom string
	TenantSiteTo   string

	Bandwidth float64
}

const capacityHeadroom = 0.05

func newReservation(edge *Edge) *BandwidthReservation {
	return &BandwidthReservation{
		ForEdge:                    edge.ID,
		FromNode:                   edge.Local,
		ToNode:                     edge.Remote,
		CapacityHeadroomPercentage: capacityHeadroom,
		Capacity:                   edge.Bandwidth * (1 - capacityHeadroom),
		Residual:                   0,
		History:                    make([]BandwidthReservationAction, 0),
	}
}

func (b *BandwidthReservationRegistry) Move(from, to []*Edge, action BandwidthReservationAction) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	delta := make(map[EdgeID]float64, len(from)+len(to))
	edges := make(map[EdgeID]*Edge, len(from)+len(to))

	for _, edge := range from {
		if _, booked := b.registry[edge.ID]; !booked {
			continue
		}

		delta[edge.ID] += action.Bandwidth
		edges[edge.ID] = edge
	}

	for _, edge := range to {
		delta[edge.ID] -= action.Bandwidth
		edges[edge.ID] = edge
	}

	for id, change := range delta {
		capacity := edges[id].Bandwidth * (1 - capacityHeadroom)
		if entry, booked := b.registry[id]; booked {
			capacity = entry.Capacity
		}

		if capacity+change < 0 {
			return fmt.Errorf("edge %s holds %.2f, needs %.2f", id, capacity, -change)
		}
	}

	for id, change := range delta {
		entry, booked := b.registry[id]
		if !booked {
			entry = newReservation(edges[id])
			b.registry[id] = entry
		}

		entry.Capacity += change
		entry.History = append(entry.History, action)
	}

	return nil
}

// CheckCapacity is idempotent: creates the entry if not exist
func (b *BandwidthReservationRegistry) CheckCapacity(edge *Edge, bandwidth float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	reservationEntry, ok := b.registry[edge.ID]
	if !ok {
		reservationEntry = newReservation(edge)
		b.registry[edge.ID] = reservationEntry
	}

	res := reservationEntry.Capacity - bandwidth
	return res >= 0
}

func (b *BandwidthReservationRegistry) PruneGraph(graph *Graph, reserveBandwidth float64) (*Graph, error) {
	pruned := &Graph{
		nodes:    graph.nodes,    // read-only
		prefixes: graph.prefixes, // read-only
		edges:    make(map[EdgeID]*Edge, len(graph.edges)),
		adj:      make(map[NodeID][]EdgeID, len(graph.adj)),
	}

	for id, edge := range graph.edges {
		if b.CheckCapacity(edge, reserveBandwidth) {
			pruned.edges[id] = edge
		}
	}

	for n, ids := range graph.adj {
		kept := slices.DeleteFunc(slices.Clone(ids), func(id EdgeID) bool {
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
	Edge     EdgeID  `json:"edge"`
	Bookable float64 `json:"bookable_mbps"`
	Booked   float64 `json:"booked_mbps"`
	Free     float64 `json:"free_mbps"`
	Bookings int     `json:"bookings"`
}

// Reports every edge in the graph: unbooked is fully free, not absent.
func (b *BandwidthReservationRegistry) Snapshot(graph *Graph) map[EdgeID]BandwidthReservationState {
	if b == nil || graph == nil {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	states := make(map[EdgeID]BandwidthReservationState, len(graph.edges))
	for id, edge := range graph.edges {
		bookable := edge.Bandwidth * (1 - capacityHeadroom)

		state := BandwidthReservationState{
			Edge:     id,
			Bookable: bookable,
			Booked:   0,
			Free:     bookable,
		}

		if entry, booked := b.registry[id]; booked {
			state.Free = entry.Capacity
			state.Booked = bookable - entry.Capacity
			state.Bookings = len(entry.History)
		}

		states[id] = state
	}

	return states
}
