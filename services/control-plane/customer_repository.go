package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type Customer struct {
	ID         int
	Allocation netip.Prefix
	VRFTable   int
	Sites      []*Site
}

type Site struct {
	CustomerID int
	CPE        string
	Node       string
	Prefix     netip.Prefix
	Attach     string
	AttachNode string
	IfID       uint32
	Identity   string
}

type CustomerRepository interface {
	Load(ctx context.Context) error
	Customer(id int) (*Customer, bool)
	Customers() []*Customer
	SiteByIdentity(identity string) (*Site, bool)
	SitesByPop(pop string) []*Site
}

type rawCustomerDB struct {
	GeneratorVersion string `json:"generator_version"`
	Customers        []struct {
		ID         int    `json:"id"`
		Allocation string `json:"allocation"`
		VRFTable   int    `json:"vrf_table"`
		Sites      []struct {
			CPE        string `json:"cpe"`
			Node       string `json:"node"`
			Prefix     string `json:"prefix"`
			Attach     string `json:"attach"`
			AttachNode string `json:"attach_node"`
			IfID       uint32 `json:"if_id"`
			Identity   string `json:"identity"`
		} `json:"sites"`
	} `json:"customers"`
}

type CustomerRepositoryConfig struct {
	StatePath       string
	TopologyDirPath string
}

type JSONCustomerRepository struct {
	config CustomerRepositoryConfig

	mu         sync.RWMutex
	byID       map[int]*Customer
	byIdentity map[string]*Site
	byPop      map[string][]*Site
}

var _ CustomerRepository = (*JSONCustomerRepository)(nil)

func NewJSONCustomerRepository(cfg CustomerRepositoryConfig) *JSONCustomerRepository {
	return &JSONCustomerRepository{
		config:     cfg,
		byID:       map[int]*Customer{},
		byIdentity: map[string]*Site{},
		byPop:      map[string][]*Site{},
	}
}

func (r *JSONCustomerRepository) Load(_ context.Context) error {
	basePath, err := readLatestTopologyBasePath(r.config.StatePath)
	if err != nil {
		return fmt.Errorf("failed to resolve build directory: %w", err)
	}

	path := filepath.Join(r.config.TopologyDirPath, basePath, CUSTOMER_DB_FILENAME)
	data, err := os.ReadFile(path) // #nosec
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	var raw rawCustomerDB
	if err = json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}

	byID := make(map[int]*Customer, len(raw.Customers))
	byIdentity := map[string]*Site{}
	byPop := map[string][]*Site{}
	seenIfID := map[uint32]string{}

	for _, rc := range raw.Customers {
		if _, exists := byID[rc.ID]; exists {
			return fmt.Errorf("duplicate customer id %d", rc.ID)
		}

		allocation, err := netip.ParsePrefix(rc.Allocation)
		if err != nil {
			return fmt.Errorf("customer %d allocation %q: %w", rc.ID, rc.Allocation, err)
		}

		customer := &Customer{
			ID:         rc.ID,
			Allocation: allocation,
			VRFTable:   rc.VRFTable,
		}

		for _, rs := range rc.Sites {
			prefix, err := netip.ParsePrefix(rs.Prefix)
			if err != nil {
				return fmt.Errorf("customer %d site %s prefix %q: %w", rc.ID, rs.CPE, rs.Prefix, err)
			}
			if !prefixWithin(allocation, prefix) {
				return fmt.Errorf("customer %d site %s prefix %s is outside allocation %s", rc.ID, rs.CPE, prefix, allocation)
			}
			if owner, taken := seenIfID[rs.IfID]; taken {
				return fmt.Errorf("if_id %d used by both %s and %s", rs.IfID, owner, rs.Identity)
			}
			if _, taken := byIdentity[rs.Identity]; taken {
				return fmt.Errorf("duplicate site identity %s", rs.Identity)
			}

			site := &Site{
				CustomerID: rc.ID,
				CPE:        rs.CPE,
				Node:       rs.Node,
				Prefix:     prefix,
				Attach:     rs.Attach,
				AttachNode: rs.AttachNode,
				IfID:       rs.IfID,
				Identity:   rs.Identity,
			}

			seenIfID[rs.IfID] = rs.Identity
			byIdentity[rs.Identity] = site
			byPop[rs.Attach] = append(byPop[rs.Attach], site)
			customer.Sites = append(customer.Sites, site)
		}

		byID[rc.ID] = customer
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID = byID
	r.byIdentity = byIdentity
	r.byPop = byPop

	return nil
}

func (r *JSONCustomerRepository) Customer(id int) (*Customer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.byID[id]
	return c, ok
}

func (r *JSONCustomerRepository) Customers() []*Customer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*Customer, 0, len(r.byID))
	for _, c := range r.byID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}

func (r *JSONCustomerRepository) SiteByIdentity(identity string) (*Site, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.byIdentity[identity]
	return s, ok
}

func (r *JSONCustomerRepository) SitesByPop(pop string) []*Site {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sites := r.byPop[pop]
	out := make([]*Site, len(sites))
	copy(out, sites)

	return out
}

func prefixWithin(outer, inner netip.Prefix) bool {
	return inner.Bits() >= outer.Bits() && outer.Contains(inner.Addr())
}
