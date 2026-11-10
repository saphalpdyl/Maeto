package nodesync

import (
	"net/netip"
	"time"
)

// TopologyReport is what a node publishes on its topology lane: the adjacency
// SIDs it has installed, keyed by the interface they forward out of.
type TopologyReport struct {
	NodeID      string            `json:"node_id"`
	ReportedAt  time.Time         `json:"reported_at"`
	Adjacencies []AdjacencyReport `json:"adjacencies"`
	// Separate from Adjacencies: a down link loses its adjacency and End.X.
	Links map[string]LinkState `json:"links,omitempty"`
}

type LinkState string

const (
	LinkStateUnknown LinkState = "UNKNOWN"
	LinkStateUp      LinkState = "UP"
	LinkStateDown    LinkState = "DOWN"
)

type AdjacencyReport struct {
	Interface string     `json:"interface"`
	EndX      netip.Addr `json:"end_x"`
}
