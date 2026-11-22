package controlplane

import (
	"maps"
	"net/netip"
	"slices"
	"sync"
	"time"
)

type NodeID string
type EdgeID string

type NodeRole string

const (
	// NodeRolePE terminates customer sites: tenant vrfs and the cpes' ipsec
	NodeRolePE NodeRole = "pe"
	// NodeRoleP is core only: igp, srv6 transit and plr repair
	NodeRoleP NodeRole = "p"
)

type Node struct {
	ID       NodeID
	Name     string
	Role     NodeRole
	ASN      uint32
	ISISNet  string
	Loopback netip.Prefix
	Locator  netip.Prefix
	Attrs    map[string]string

	MirrorLocator netip.Prefix

	ProtectedBy []NodeID
	ProtectorOf []NodeID
}

type Edge struct {
	ID          EdgeID
	Local       NodeID
	Remote      NodeID
	Role        string
	LocalIface  string
	RemoteIface string
	LocalAddr   string
	RemoteAddr  string
	Subnet      string
	Metric      int
	TEMetric    int
	// Bandwidth is the link's capacity in Mbps.
	Bandwidth float64
	Delay     time.Duration
	Up        bool

	EndX netip.Addr
	// Last know EndX. During failure, the controller set EndX to empty
	// We want to keep on building the repairList for the PLR
	// while the pe-down/link-down event is occurring.
	LastEndX netip.Addr
}

type Prefix struct {
	Origin NodeID
	Subnet string
	Attrs  map[string]string
}

type SRv6DomainMetadata struct {
	LocatorPrefix netip.Prefix
	LinkPrefix    netip.Prefix
	EdgePrefix    netip.Prefix
}

type Graph struct {
	mu       sync.RWMutex
	nodes    map[NodeID]*Node
	edges    map[EdgeID]*Edge
	prefixes map[string]*Prefix
	adj      map[NodeID][]EdgeID

	// State
	down map[NodeID]bool
}

// Clone returns a deep copy: nothing in the copy shares memory with g, so it
// can be read and changed without g's lock.
func (g *Graph) Clone() *Graph {
	g.mu.RLock()
	defer g.mu.RUnlock()

	newGraph := &Graph{
		nodes:    make(map[NodeID]*Node, len(g.nodes)),
		edges:    make(map[EdgeID]*Edge, len(g.edges)),
		prefixes: make(map[string]*Prefix, len(g.prefixes)),
		adj:      make(map[NodeID][]EdgeID, len(g.adj)),
	}

	for id, node := range g.nodes {
		nodeCopy := *node
		nodeCopy.Attrs = maps.Clone(node.Attrs)
		nodeCopy.ProtectedBy = slices.Clone(node.ProtectedBy)
		nodeCopy.ProtectorOf = slices.Clone(node.ProtectorOf)
		newGraph.nodes[id] = &nodeCopy
	}

	for id, edge := range g.edges {
		edgeCopy := *edge
		newGraph.edges[id] = &edgeCopy
	}

	for key, prefix := range g.prefixes {
		prefixCopy := *prefix
		prefixCopy.Attrs = maps.Clone(prefix.Attrs)
		newGraph.prefixes[key] = &prefixCopy
	}

	for id, edgeIDs := range g.adj {
		newGraph.adj[id] = slices.Clone(edgeIDs)
	}

	newGraph.down = maps.Clone(g.down)

	return newGraph
}

func (g *Graph) LostAllNeighbors(node NodeID, fresh map[NodeID]bool) bool {
	witnesses := 0
	for _, edge := range g.edges {
		if edge.Remote != node || !fresh[edge.Local] {
			continue
		}

		if edge.Up {
			return false
		}

		witnesses++
	}

	return witnesses > 0
}

// EffectivePE returns the protector node IDs if the designated Node is down
func (g *Graph) EffectivePE(node NodeID) (NodeID, bool) {
	if !g.down[node] {
		return node, true
	}

	for _, protectorNode := range g.nodes[node].ProtectedBy {
		if !g.down[protectorNode] {
			return protectorNode, true
		}
	}

	return "", false
}

type TopologyManager interface {
	LoadTopology() error
	IsReady() bool

	// Get SRv6 domain metadata such as SRv6 domain prefix
	GetDomainMetadata() SRv6DomainMetadata

	SetDownNodes(fresh map[NodeID]bool, ready map[NodeID]bool) []NodeID
}
