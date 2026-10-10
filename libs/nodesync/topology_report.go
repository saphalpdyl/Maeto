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
}

type AdjacencyReport struct {
	Interface string     `json:"interface"`
	EndX      netip.Addr `json:"end_x"`
}
