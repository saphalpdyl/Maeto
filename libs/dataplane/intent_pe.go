package dataplane

import "net/netip"

// Bad naming convention: Portal-specific configuration done on PE's dataplane
type PE_PortalIntent struct {
	HostFacingInterface string `json:"host_facing_interface"`
	TunnelInterfaceID   uint32 `json:"tunnel_interface_id"`

	SitePrefix   netip.Prefix `json:"site_prefix"`
	TenantPrefix netip.Prefix `json:"tenant_prefix"`
}

type PEIntent struct {
	NodeID string `json:"node_id"`

	// Multiple tenants can have multiple sites. One site is represented in Maeto by
	// the presence of only ONE maeto-portal device
	//
	// 1st key = tenant_id
	//
	// 2nd key = portal_id
	Tenants map[string]map[string]PE_PortalIntent `json:"tenants"`
}

func (*PEIntent) isIntent() {}

func (n *NodeIntent) Clone() *NodeIntent {
	if n == nil {
		return nil
	}

	out := *n
	if n.Intent != nil {
		out.Intent = n.Intent.Clone()
	}

	return &out
}

func (i *PEIntent) Clone() Intent {
	if i == nil {
		return nil
	}

	out := *i

	// Tenants is two levels of map, and a struct copy would share both
	out.Tenants = make(map[string]map[string]PE_PortalIntent, len(i.Tenants))
	for tenantID, portals := range i.Tenants {
		copied := make(map[string]PE_PortalIntent, len(portals))
		for portalID, portal := range portals {
			copied[portalID] = portal // PE_PortalIntent is all value types
		}
		out.Tenants[tenantID] = copied
	}

	return &out
}
