// Holds facts about the nodes themselves, kept out of the graph so path
// computation stays core-only
package controlplane

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"sync"
)

type NodeAccess struct {
	Interface string
	Address   netip.Prefix
	Aggregate netip.Prefix
	Nexthop   netip.Addr
}

type NodeInterface struct {
	Name    string
	Role    string
	Peer    string
	Address string
}

type InventoryNode struct {
	ID         NodeID
	Name       string
	Index      int
	ISISNet    string
	Locator    string
	Loopback   string
	Interfaces []NodeInterface
	Access     *NodeAccess
}

func (n *InventoryNode) HasAccessSide() bool {
	return n.Access != nil
}

type NodeInventory interface {
	Load(ctx context.Context) error
	Node(id NodeID) (*InventoryNode, bool)
	NodeByName(name string) (*InventoryNode, bool)
	Nodes() []*InventoryNode
}

type NodeInventoryConfig struct {
	StatePath       string
	TopologyDirPath string
}

type JSONNodeInventory struct {
	config NodeInventoryConfig

	mu     sync.RWMutex
	byID   map[NodeID]*InventoryNode
	byName map[string]*InventoryNode
}

var _ NodeInventory = (*JSONNodeInventory)(nil)

func NewJSONNodeInventory(cfg NodeInventoryConfig) *JSONNodeInventory {
	return &JSONNodeInventory{
		config: cfg,
		byID:   map[NodeID]*InventoryNode{},
		byName: map[string]*InventoryNode{},
	}
}

func (r *JSONNodeInventory) Load(_ context.Context) error {
	raw, err := readLatestTopology(r.config.StatePath, r.config.TopologyDirPath)
	if err != nil {
		return fmt.Errorf("failed to read topology data: %w", err)
	}

	byID := make(map[NodeID]*InventoryNode, len(raw.Pops))
	byName := make(map[string]*InventoryNode, len(raw.Pops))

	for _, p := range raw.Pops {
		id := NodeID(p.ID)
		if _, exists := byID[id]; exists {
			return fmt.Errorf("duplicate node id %s", p.ID)
		}
		if _, exists := byName[p.Name]; exists {
			return fmt.Errorf("duplicate node name %s", p.Name)
		}

		node := &InventoryNode{
			ID:         id,
			Name:       p.Name,
			Index:      p.Index,
			ISISNet:    p.IsisNet,
			Locator:    p.Locator,
			Loopback:   p.Loopback,
			Interfaces: make([]NodeInterface, 0, len(p.Interfaces)),
		}

		for _, i := range p.Interfaces {
			node.Interfaces = append(node.Interfaces, NodeInterface{
				Name:    i.Name,
				Role:    i.Role,
				Peer:    i.Peer,
				Address: i.Address,
			})
		}

		if p.Access != nil {
			address, err := netip.ParsePrefix(p.Access.Address)
			if err != nil {
				return fmt.Errorf("node %s access address %q: %w", p.ID, p.Access.Address, err)
			}

			aggregate, err := netip.ParsePrefix(p.Access.Aggregate)
			if err != nil {
				return fmt.Errorf("node %s access aggregate %q: %w", p.ID, p.Access.Aggregate, err)
			}

			nexthop, err := netip.ParseAddr(p.Access.Nexthop)
			if err != nil {
				return fmt.Errorf("node %s access nexthop %q: %w", p.ID, p.Access.Nexthop, err)
			}

			if !prefixWithin(aggregate, address) {
				return fmt.Errorf("node %s access address %s is outside aggregate %s", p.ID, address, aggregate)
			}
			if !aggregate.Contains(nexthop) {
				return fmt.Errorf("node %s access nexthop %s is outside aggregate %s", p.ID, nexthop, aggregate)
			}

			node.Access = &NodeAccess{
				Interface: p.Access.Interface,
				Address:   address,
				Aggregate: aggregate,
				Nexthop:   nexthop,
			}
		}

		byID[id] = node
		byName[p.Name] = node
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID = byID
	r.byName = byName

	return nil
}

func (r *JSONNodeInventory) Node(id NodeID) (*InventoryNode, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	n, ok := r.byID[id]
	return n, ok
}

func (r *JSONNodeInventory) NodeByName(name string) (*InventoryNode, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	n, ok := r.byName[name]
	return n, ok
}

func (r *JSONNodeInventory) Nodes() []*InventoryNode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*InventoryNode, 0, len(r.byID))
	for _, n := range r.byID {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}
