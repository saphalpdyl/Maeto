package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
)

type TenantID int

const (
	DefaultClassName         = "default"
	DefaultClassLabel uint64 = 0
	DefaultClassIndex        = 0
	UnrankedTierIndex        = 0
)

const (
	DSCPUnmarked int = 0
	DSCPMax      int = 63
)

type TrafficClass struct {
	Name      string
	Label     uint64
	Index     int
	Match     ClassMatch
	Objective map[CostDimension]float64
}

type ClassMatch struct {
	DSCP []int
}

type Tenant struct {
	ID         TenantID
	Allocation netip.Prefix
	// Tier is the tenant's service class, carried verbatim from the topology.
	Tier      string
	TierIndex int
	VRFTable  int
	Sites     []*Site
}

type Site struct {
	TenantID   TenantID
	CPE        string
	PortalID   string
	Node       string
	Prefix     netip.Prefix
	Attach     string
	AttachNode string
	IfID       uint32
	Identity   string
	// ReservationMbps is the bandwidth this site has reserved.
	ReservationMbps float64
	ClassMbps       map[uint64]float64
}

type TenantRepository interface {
	Load(ctx context.Context) error
	Tenant(id TenantID) (*Tenant, bool)
	Tenants() []*Tenant
	TenantsByPriority() []*Tenant
	Classes() []*TrafficClass
	Class(label uint64) (*TrafficClass, bool)
	SiteByIdentity(identity string) (*Site, bool)
	SiteByPortalID(portalID string) (*Site, bool)
	SitesByPop(pop string) []*Site
}

type rawTenantDB struct {
	GeneratorVersion string            `json:"generator_version"`
	Classes          []rawTrafficClass `json:"classes"`
	Tiers            []rawTier         `json:"tiers"`
	Tenants          []struct {
		ID         TenantID `json:"id"`
		Allocation string   `json:"allocation"`
		Tier       string   `json:"tier"`
		VRFTable   int      `json:"vrf_table"`
		Sites      []struct {
			CPE             string             `json:"cpe"`
			PortalID        string             `json:"portal_id"`
			Node            string             `json:"node"`
			Prefix          string             `json:"prefix"`
			Attach          string             `json:"attach"`
			AttachNode      string             `json:"attach_node"`
			IfID            uint32             `json:"if_id"`
			Identity        string             `json:"identity"`
			ReservationMbps float64            `json:"reservation_mbps"`
			Classes         map[string]float64 `json:"classes"`
		} `json:"sites"`
	} `json:"tenants"`
}

type rawTrafficClass struct {
	Name      string             `json:"name"`
	Label     uint64             `json:"label"`
	Index     int                `json:"index"`
	Match     rawClassMatch      `json:"match"`
	Objective map[string]float64 `json:"objective"`
}

type rawClassMatch struct {
	DSCP []int `json:"dscp"`
}

type rawTier struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

type TenantRepositoryConfig struct {
	StatePath       string
	TopologyDirPath string
}

type JSONTenantRepository struct {
	config TenantRepositoryConfig

	mu         sync.RWMutex
	classes    map[uint64]*TrafficClass
	byID       map[TenantID]*Tenant
	byIdentity map[string]*Site
	byPortalID map[string]*Site
	byPop      map[string][]*Site
}

var _ TenantRepository = (*JSONTenantRepository)(nil)

func NewJSONTenantRepository(cfg TenantRepositoryConfig) *JSONTenantRepository {
	return &JSONTenantRepository{
		config:     cfg,
		classes:    map[uint64]*TrafficClass{},
		byID:       map[TenantID]*Tenant{},
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

	classes, classByName, err := parseTrafficClasses(raw.Classes)
	if err != nil {
		return err
	}

	tierIndex, err := parseTiers(raw.Tiers)
	if err != nil {
		return err
	}

	byID := make(map[TenantID]*Tenant, len(raw.Tenants))
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
			Tier:       rc.Tier,
			TierIndex:  UnrankedTierIndex,
			VRFTable:   rc.VRFTable,
		}
		if index, ranked := tierIndex[rc.Tier]; ranked {
			tenant.TierIndex = index
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

			classMbps, err := resolveSiteClasses(rs.Classes, rs.ReservationMbps, classByName)
			if err != nil {
				return fmt.Errorf("tenant %d site %s: %w", rc.ID, rs.CPE, err)
			}

			site := &Site{
				TenantID:        rc.ID,
				CPE:             rs.CPE,
				PortalID:        rs.PortalID,
				Node:            rs.Node,
				Prefix:          prefix,
				Attach:          rs.Attach,
				AttachNode:      rs.AttachNode,
				IfID:            rs.IfID,
				Identity:        rs.Identity,
				ReservationMbps: rs.ReservationMbps,
				ClassMbps:       classMbps,
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
	r.classes = classes
	r.byID = byID
	r.byIdentity = byIdentity
	r.byPortalID = byPortalID
	r.byPop = byPop

	return nil
}

func (r *JSONTenantRepository) Tenant(id TenantID) (*Tenant, bool) {
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

func (r *JSONTenantRepository) TenantsByPriority() []*Tenant {
	out := r.Tenants()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TierIndex != out[j].TierIndex {
			return out[i].TierIndex > out[j].TierIndex
		}
		return out[i].ID < out[j].ID
	})

	return out
}

func (r *JSONTenantRepository) Classes() []*TrafficClass {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*TrafficClass, 0, len(r.classes))
	for _, c := range r.classes {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })

	return out
}

func (r *JSONTenantRepository) Class(label uint64) (*TrafficClass, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.classes[label]
	return c, ok
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

func parseTrafficClasses(raw []rawTrafficClass) (map[uint64]*TrafficClass, map[string]*TrafficClass, error) {
	byLabel := map[uint64]*TrafficClass{
		DefaultClassLabel: {Name: DefaultClassName, Label: DefaultClassLabel, Index: DefaultClassIndex},
	}
	byName := map[string]*TrafficClass{
		DefaultClassName: byLabel[DefaultClassLabel],
	}
	byIndex := map[int]string{}
	byDSCP := map[int]string{}

	for _, rc := range raw {
		if rc.Name == "" {
			return nil, nil, fmt.Errorf("class with label %d has no name", rc.Label)
		}
		if owner, taken := byName[rc.Name]; taken {
			return nil, nil, fmt.Errorf("class name %q already used by label %d", rc.Name, owner.Label)
		}
		if owner, taken := byLabel[rc.Label]; taken {
			return nil, nil, fmt.Errorf("class %s label %d already used by class %s", rc.Name, rc.Label, owner.Name)
		}
		if rc.Index < 1 {
			return nil, nil, fmt.Errorf("class %s index must be at least 1", rc.Name)
		}
		if owner, taken := byIndex[rc.Index]; taken {
			return nil, nil, fmt.Errorf("class %s index %d already used by class %s", rc.Name, rc.Index, owner)
		}
		if len(rc.Objective) == 0 {
			return nil, nil, fmt.Errorf("class %s has no objective", rc.Name)
		}

		objective := make(map[CostDimension]float64, len(rc.Objective))
		for dim, weight := range rc.Objective {
			costDim, err := parseCostDimension(dim)
			if err != nil {
				return nil, nil, fmt.Errorf("class %s: %w", rc.Name, err)
			}
			if weight <= 0 {
				return nil, nil, fmt.Errorf("class %s objective %s weight must be greater than zero", rc.Name, dim)
			}
			objective[costDim] = weight
		}

		match, err := parseClassMatch(rc.Name, rc.Match, byDSCP)
		if err != nil {
			return nil, nil, err
		}

		class := &TrafficClass{Name: rc.Name, Label: rc.Label, Index: rc.Index, Match: match, Objective: objective}
		byLabel[rc.Label] = class
		byName[rc.Name] = class
		byIndex[rc.Index] = rc.Name
	}

	return byLabel, byName, nil
}

func parseClassMatch(name string, raw rawClassMatch, byDSCP map[int]string) (ClassMatch, error) {
	if len(raw.DSCP) == 0 {
		return ClassMatch{}, fmt.Errorf("class %s matches no dscp codepoints", name)
	}

	dscp := make([]int, 0, len(raw.DSCP))
	for _, value := range raw.DSCP {
		if value < 0 || value > int(DSCPMax) {
			return ClassMatch{}, fmt.Errorf("class %s dscp %d is outside 0..%d", name, value, DSCPMax)
		}

		codepoint := value
		if codepoint == DSCPUnmarked {
			return ClassMatch{}, fmt.Errorf("class %s dscp %d is unmarked traffic and always rides the %s class", name, value, DefaultClassName)
		}
		if owner, taken := byDSCP[codepoint]; taken {
			return ClassMatch{}, fmt.Errorf("class %s dscp %d already matched by class %s", name, value, owner)
		}

		byDSCP[codepoint] = name
		dscp = append(dscp, codepoint)
	}

	slices.Sort(dscp)

	return ClassMatch{DSCP: dscp}, nil
}

func parseTiers(raw []rawTier) (map[string]int, error) {
	byName := make(map[string]int, len(raw))
	byIndex := make(map[int]string, len(raw))

	for _, rt := range raw {
		if rt.Name == "" {
			return nil, fmt.Errorf("tier with index %d has no name", rt.Index)
		}
		if _, taken := byName[rt.Name]; taken {
			return nil, fmt.Errorf("duplicate tier %q", rt.Name)
		}
		if rt.Index < 1 {
			return nil, fmt.Errorf("tier %s index must be at least 1", rt.Name)
		}
		if owner, taken := byIndex[rt.Index]; taken {
			return nil, fmt.Errorf("tier %s index %d already used by tier %s", rt.Name, rt.Index, owner)
		}

		byName[rt.Name] = rt.Index
		byIndex[rt.Index] = rt.Name
	}

	return byName, nil
}

func parseCostDimension(dim string) (CostDimension, error) {
	switch d := CostDimension(strings.ToUpper(dim)); d {
	case COSTDIM_LATENCY, COSTDIM_JITTER, COSTDIM_LOSS:
		return d, nil
	default:
		return "", fmt.Errorf("unknown objective dimension %q", dim)
	}
}

const classMbpsTolerance = 1e-6

func resolveSiteClasses(raw map[string]float64, reservation float64, classByName map[string]*TrafficClass) (map[uint64]float64, error) {
	out := make(map[uint64]float64, len(raw)+1)
	carved := 0.0

	for name, mbps := range raw {
		class, ok := classByName[name]
		if !ok {
			return nil, fmt.Errorf("unknown class %q", name)
		}
		if class.Label == DefaultClassLabel {
			return nil, fmt.Errorf("class %q is implicit and takes the remainder", DefaultClassName)
		}
		if mbps <= 0 {
			return nil, fmt.Errorf("class %s must reserve more than 0 Mbps", name)
		}

		out[class.Label] = mbps
		carved += mbps
	}

	remainder := reservation - carved
	if remainder < -classMbpsTolerance {
		return nil, fmt.Errorf("classes reserve %.3f Mbps, more than the site's %.3f", carved, reservation)
	}
	if remainder > classMbpsTolerance {
		out[DefaultClassLabel] = remainder
	}

	return out, nil
}

func prefixWithin(outer, inner netip.Prefix) bool {
	return inner.Bits() >= outer.Bits() && outer.Contains(inner.Addr())
}
