package controlplane

import (
	"net/netip"
	"sync"
	"time"
)

type NodeID string
type EdgeID string

type Node struct {
	ID       NodeID
	Name     string
	ASN      uint32
	ISISNet  string
	Loopback netip.Prefix
	Locator  netip.Prefix
	Attrs    map[string]string
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
	Bandwidth   float64
	Delay       time.Duration
	Up          bool
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
	mu       sync.RWMutex //nolint:unused // guards graph once concurrent access lands
	nodes    map[NodeID]*Node
	edges    map[EdgeID]*Edge
	prefixes map[string]*Prefix
	adj      map[NodeID][]EdgeID
}

func (g *Graph) Clone() *Graph {
	g.mu.Lock()
	defer g.mu.Unlock()

	newGraph := &Graph{
		nodes:    make(map[NodeID]*Node),
		edges:    make(map[EdgeID]*Edge),
		prefixes: make(map[string]*Prefix),
		adj:      make(map[NodeID][]EdgeID),
	}

	for k, v := range g.nodes {
		newGraph.nodes[k] = v
	}

	for k, v := range g.edges {
		newGraph.edges[k] = v
	}

	for k, v := range g.prefixes {
		newGraph.prefixes[k] = v
	}

	for k, v := range g.adj {
		newGraph.adj[k] = v
	}

	return newGraph
}

type TopologyManager interface {
	LoadTopology() error
	IsReady() bool

	// Get SRv6 domain metadata such as SRv6 domain prefix
	GetDomainMetadata() SRv6DomainMetadata
}
