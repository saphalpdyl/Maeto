// AI-generated: too lazy to write these
package controlplane

import (
	"context"
	"log/slog"
	"net/netip"
	"sort"
	"time"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/statekv"
	log "github.com/saphalpdyl/maeto/services/control-plane/log"
)

type TopologyNodeSnapshot struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	ASN      uint32            `json:"asn"`
	ISISNet  string            `json:"isis_net"`
	Loopback string            `json:"loopback"`
	Locator  string            `json:"locator"`
	Attrs    map[string]string `json:"attrs,omitempty"`
}

type TopologyEdgeSnapshot struct {
	ID          string  `json:"id"`
	Local       string  `json:"local"`
	Remote      string  `json:"remote"`
	Role        string  `json:"role"`
	LocalIface  string  `json:"local_iface"`
	RemoteIface string  `json:"remote_iface"`
	LocalAddr   string  `json:"local_addr"`
	RemoteAddr  string  `json:"remote_addr"`
	Subnet      string  `json:"subnet"`
	Metric      int     `json:"metric"`
	TEMetric    int     `json:"te_metric"`
	Bandwidth   float64 `json:"bandwidth"`
	DelayMS     float64 `json:"delay_ms"`
	Up          bool    `json:"up"`
}

type TopologyPrefixSnapshot struct {
	Origin string            `json:"origin"`
	Subnet string            `json:"subnet"`
	Attrs  map[string]string `json:"attrs,omitempty"`
}

type DomainSnapshot struct {
	LocatorPrefix string `json:"locator_prefix"`
	LinkPrefix    string `json:"link_prefix"`
	EdgePrefix    string `json:"edge_prefix"`
}

type TopologySnapshot struct {
	Nodes    []TopologyNodeSnapshot   `json:"nodes"`
	Edges    []TopologyEdgeSnapshot   `json:"edges"`
	Prefixes []TopologyPrefixSnapshot `json:"prefixes"`
	Domain   DomainSnapshot           `json:"domain"`
}

type InventoryAccessSnapshot struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Aggregate string `json:"aggregate"`
	Nexthop   string `json:"nexthop"`
}

type InventoryNodeSnapshot struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Index      int                      `json:"index"`
	ISISNet    string                   `json:"isis_net"`
	Locator    string                   `json:"locator"`
	Loopback   string                   `json:"loopback"`
	Interfaces []NodeInterface          `json:"interfaces"`
	Access     *InventoryAccessSnapshot `json:"access,omitempty"`
}

type RegistrySnapshot struct {
	Nodes         map[string]*dataplane.NodeIntent `json:"nodes"`
	SIDCursor     uint16                           `json:"sid_cursor"`
	SIDsByTenant  map[string]string                `json:"sids_by_tenant"`
	AllocatedSIDs []string                         `json:"allocated_sids"`
}

type ControlSnapshot struct {
	PublishedAt time.Time               `json:"published_at"`
	Topology    TopologySnapshot        `json:"topology"`
	Inventory   []InventoryNodeSnapshot `json:"inventory"`
	Registry    RegistrySnapshot        `json:"registry"`
}

func SnapshotTopology(graph *Graph, domain SRv6DomainMetadata) TopologySnapshot {
	snapshot := TopologySnapshot{
		Nodes:    []TopologyNodeSnapshot{},
		Edges:    []TopologyEdgeSnapshot{},
		Prefixes: []TopologyPrefixSnapshot{},
		Domain: DomainSnapshot{
			LocatorPrefix: prefixString(domain.LocatorPrefix),
			LinkPrefix:    prefixString(domain.LinkPrefix),
			EdgePrefix:    prefixString(domain.EdgePrefix),
		},
	}

	if graph == nil {
		return snapshot
	}

	graph.mu.RLock()
	defer graph.mu.RUnlock()

	for _, node := range graph.nodes {
		snapshot.Nodes = append(snapshot.Nodes, TopologyNodeSnapshot{
			ID:       string(node.ID),
			Name:     node.Name,
			ASN:      node.ASN,
			ISISNet:  node.ISISNet,
			Loopback: prefixString(node.Loopback),
			Locator:  prefixString(node.Locator),
			Attrs:    node.Attrs,
		})
	}

	for _, edge := range dedupeEdges(graph.edges) {
		snapshot.Edges = append(snapshot.Edges, TopologyEdgeSnapshot{
			ID:          string(edge.ID),
			Local:       string(edge.Local),
			Remote:      string(edge.Remote),
			Role:        edge.Role,
			LocalIface:  edge.LocalIface,
			RemoteIface: edge.RemoteIface,
			LocalAddr:   edge.LocalAddr,
			RemoteAddr:  edge.RemoteAddr,
			Subnet:      edge.Subnet,
			Metric:      edge.Metric,
			TEMetric:    edge.TEMetric,
			Bandwidth:   edge.Bandwidth,
			DelayMS:     float64(edge.Delay) / float64(time.Millisecond),
			Up:          edge.Up,
		})
	}

	for _, prefix := range graph.prefixes {
		snapshot.Prefixes = append(snapshot.Prefixes, TopologyPrefixSnapshot{
			Origin: string(prefix.Origin),
			Subnet: prefix.Subnet,
			Attrs:  prefix.Attrs,
		})
	}

	sort.Slice(snapshot.Nodes, func(i, j int) bool { return snapshot.Nodes[i].ID < snapshot.Nodes[j].ID })
	sort.Slice(snapshot.Edges, func(i, j int) bool { return snapshot.Edges[i].ID < snapshot.Edges[j].ID })
	sort.Slice(snapshot.Prefixes, func(i, j int) bool {
		return snapshot.Prefixes[i].Subnet < snapshot.Prefixes[j].Subnet
	})

	return snapshot
}

func dedupeEdges(edges map[EdgeID]*Edge) []*Edge {
	kept := make(map[string]*Edge, len(edges))

	for _, edge := range edges {
		local := string(edge.Local) + ":" + edge.LocalIface
		remote := string(edge.Remote) + ":" + edge.RemoteIface

		key := local + "|" + remote
		if remote < local {
			key = remote + "|" + local
		}

		existing, seen := kept[key]
		if !seen || local < string(existing.Local)+":"+existing.LocalIface {
			kept[key] = edge
		}
	}

	out := make([]*Edge, 0, len(kept))
	for _, edge := range kept {
		out = append(out, edge)
	}

	return out
}

func SnapshotInventory(inventory NodeInventory) []InventoryNodeSnapshot {
	out := []InventoryNodeSnapshot{}
	if inventory == nil {
		return out
	}

	for _, node := range inventory.Nodes() {
		entry := InventoryNodeSnapshot{
			ID:         string(node.ID),
			Name:       node.Name,
			Index:      node.Index,
			ISISNet:    node.ISISNet,
			Locator:    node.Locator,
			Loopback:   node.Loopback,
			Interfaces: node.Interfaces,
		}

		if node.Access != nil {
			entry.Access = &InventoryAccessSnapshot{
				Interface: node.Access.Interface,
				Address:   prefixString(node.Access.Address),
				Aggregate: prefixString(node.Access.Aggregate),
				Nexthop:   node.Access.Nexthop.String(),
			}
		}

		out = append(out, entry)
	}

	return out
}

func (r *ServiceRegistry) Snapshot() RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make(map[string]*dataplane.NodeIntent, len(r.registry))
	for id, intent := range r.registry {
		nodes[id] = intent.Clone()
	}

	byTenant := make(map[string]string, len(r.sidTenantMap))
	for key, sid := range r.sidTenantMap {
		byTenant[key] = sid.String()
	}

	allocated := make([]string, 0, len(r.sidAllocationMap))
	for sid, taken := range r.sidAllocationMap {
		if taken {
			allocated = append(allocated, sid.String())
		}
	}
	sort.Strings(allocated)

	return RegistrySnapshot{
		Nodes:         nodes,
		SIDCursor:     r.sidCursor,
		SIDsByTenant:  byTenant,
		AllocatedSIDs: allocated,
	}
}

type SnapshotPublisher struct {
	publisher *statekv.Publisher
	interval  time.Duration
	logger    *slog.Logger

	graph     *Graph
	domain    SRv6DomainMetadata
	inventory NodeInventory
	registry  *ServiceRegistry
}

func NewSnapshotPublisher(
	publisher *statekv.Publisher,
	interval time.Duration,
	logger *slog.Logger,
	graph *Graph,
	domain SRv6DomainMetadata,
	inventory NodeInventory,
	registry *ServiceRegistry,
) *SnapshotPublisher {
	return &SnapshotPublisher{
		publisher: publisher,
		interval:  interval,
		logger:    logger,
		graph:     graph,
		domain:    domain,
		inventory: inventory,
		registry:  registry,
	}
}

func (s *SnapshotPublisher) Snapshot() ControlSnapshot {
	snapshot := ControlSnapshot{
		PublishedAt: time.Now(),
		Topology:    SnapshotTopology(s.graph, s.domain),
		Inventory:   SnapshotInventory(s.inventory),
	}

	if s.registry != nil {
		snapshot.Registry = s.registry.Snapshot()
	}

	return snapshot
}

func (s *SnapshotPublisher) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.publish(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.publish(ctx)
		}
	}
}

func (s *SnapshotPublisher) publish(ctx context.Context) {
	if _, err := s.publisher.Publish(ctx, statekv.KeyControlSnapshot, s.Snapshot()); err != nil {
		s.logger.ErrorContext(ctx, "failed to publish control snapshot", log.Err(err))
	}
}

func prefixString(prefix netip.Prefix) string {
	if !prefix.IsValid() {
		return ""
	}

	return prefix.String()
}
