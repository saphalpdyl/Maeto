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

type Tenant struct {
	ID         int
	Allocation netip.Prefix
	VRFTable   int
	Sites      []*Site
}

type Site struct {
	TenantID   int
	CPE        string
	PortalID   string
	Node       string
	Prefix     netip.Prefix
	Attach     string
	AttachNode string
	IfID       uint32
	Identity   string
}

type TenantRepository interface {
	Load(ctx context.Context) error
	Tenant(id int) (*Tenant, bool)
	Tenants() []*Tenant
	SiteByIdentity(identity string) (*Site, bool)
	SiteByPortalID(portalID string) (*Site, bool)
	SitesByPop(pop string) []*Site
}

type rawTenantDB struct {
	GeneratorVersion string `json:"generator_version"`
	Tenants          []struct {
		ID         int    `json:"id"`
		Allocation string `json:"allocation"`
		VRFTable   int    `json:"vrf_table"`
		Sites      []struct {
			CPE        string `json:"cpe"`
			PortalID   string `json:"portal_id"`
			Node       string `json:"node"`
			Prefix     string `json:"prefix"`
			Attach     string `json:"attach"`
			AttachNode string `json:"attach_node"`
			IfID       uint32 `json:"if_id"`
			Identity   string `json:"identity"`
		} `json:"sites"`
	} `json:"tenants"`
}

type TenantRepositoryConfig struct {
	StatePath       string
	TopologyDirPath string
}

type JSONTenantRepository struct {
	config TenantRepositoryConfig

	mu         sync.RWMutex
	byID       map[int]*Tenant
	byIdentity map[string]*Site
	byPortalID map[string]*Site
	byPop      map[string][]*Site
}

var _ TenantRepository = (*JSONTenantRepository)(nil)

func NewJSONTenantRepository(cfg TenantRepositoryConfig) *JSONTenantRepository {
	return &JSONTenantRepository{
		config:     cfg,
		byID:       map[int]*Tenant{},
		byIdentity: map[string]*Site{},
		byPortalID: map[string]*Site{},
		byPop:      map[string][]*Site{},
	}
}

func (r *JSONTenantRepository) Load(_ context.Context) error {
	basePath, err := readLatestTopologyBasePath(r.config.StatePath)
	if err != nil {
		return fmt.Errorf("failed to resolve build directory: %w", err)
	}

	path := filepath.Join(r.config.TopologyDirPath, basePath, TENANT_DB_FILENAME)
	data, err := os.ReadFile(path) // #nosec
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	var raw rawTenantDB
	if err = json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}

	byID := make(map[int]*Tenant, len(raw.Tenants))
	byIdentity := map[string]*Site{}
	byPortalID := map[string]*Site{}
	byPop := map[string][]*Site{}
	seenIfID := map[uint32]string{}

	for _, rc := range raw.Tenants {
		if _, exists := byID[rc.ID]; exists {
			return fmt.Errorf("duplicate tenant id %d", rc.ID)
		}

		allocation, err := netip.ParsePrefix(rc.Allocation)
		if err != nil {
			return fmt.Errorf("tenant %d allocation %q: %w", rc.ID, rc.Allocation, err)
		}

		tenant := &Tenant{
			ID:         rc.ID,
			Allocation: allocation,
			VRFTable:   rc.VRFTable,
		}

		for _, rs := range rc.Sites {
			prefix, err := netip.ParsePrefix(rs.Prefix)
			if err != nil {
				return fmt.Errorf("tenant %d site %s prefix %q: %w", rc.ID, rs.CPE, rs.Prefix, err)
			}
			if !prefixWithin(allocation, prefix) {
				return fmt.Errorf("tenant %d site %s prefix %s is outside allocation %s", rc.ID, rs.CPE, prefix, allocation)
			}
			if owner, taken := seenIfID[rs.IfID]; taken {
				return fmt.Errorf("if_id %d used by both %s and %s", rs.IfID, owner, rs.Identity)
			}
			if _, taken := byIdentity[rs.Identity]; taken {
				return fmt.Errorf("duplicate site identity %s", rs.Identity)
			}
			if owner, taken := byPortalID[rs.PortalID]; taken {
				return fmt.Errorf("portal id %s used by both %s and %s", rs.PortalID, owner.Identity, rs.Identity)
			}

			site := &Site{
				TenantID:   rc.ID,
				CPE:        rs.CPE,
				PortalID:   rs.PortalID,
				Node:       rs.Node,
				Prefix:     prefix,
				Attach:     rs.Attach,
				AttachNode: rs.AttachNode,
				IfID:       rs.IfID,
				Identity:   rs.Identity,
			}

			seenIfID[rs.IfID] = rs.Identity
			byIdentity[rs.Identity] = site
			byPortalID[rs.PortalID] = site
			byPop[rs.Attach] = append(byPop[rs.Attach], site)
			tenant.Sites = append(tenant.Sites, site)
		}

		byID[rc.ID] = tenant
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID = byID
	r.byIdentity = byIdentity
	r.byPortalID = byPortalID
	r.byPop = byPop

	return nil
}

func (r *JSONTenantRepository) Tenant(id int) (*Tenant, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.byID[id]
	return c, ok
}

func (r *JSONTenantRepository) Tenants() []*Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*Tenant, 0, len(r.byID))
	for _, c := range r.byID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}

func (r *JSONTenantRepository) SiteByIdentity(identity string) (*Site, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.byIdentity[identity]
	return s, ok
}

func (r *JSONTenantRepository) SiteByPortalID(portalID string) (*Site, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.byPortalID[portalID]
	return s, ok
}

func (r *JSONTenantRepository) SitesByPop(pop string) []*Site {
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
