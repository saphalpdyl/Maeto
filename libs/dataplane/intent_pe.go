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
	// key = tenant_id
	Tenants map[string]TenantIntent `json:"tenants"`
}

// TenantIntent is one tenant's footprint on this node: every site of theirs that
// lands here, plus the single SID that gets them into their vrf.
type TenantIntent struct {
	// key = portal_id
	PortalIntents map[string]PE_PortalIntent `json:"portals"`

	// End.DT46 decapsulates and looks the inner packet up in the tenant's vrf,
	// so the SID identifies the vrf, not the site. One per tenant per node --
	// every portal below shares it.
	DT46SID netip.Addr `json:"dt46_sid"`
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

	// a struct copy would share the tenant map and every portal map hanging off it
	out.Tenants = make(map[string]TenantIntent, len(i.Tenants))
	for tenantID, tenant := range i.Tenants {
		copied := make(map[string]PE_PortalIntent, len(tenant.PortalIntents))
		for portalID, portal := range tenant.PortalIntents {
			copied[portalID] = portal // PE_PortalIntent is all value types
		}

		// tenant is already a copy of the value, so swapping the one reference
		// type on it carries DT46SID across untouched
		tenant.PortalIntents = copied
		out.Tenants[tenantID] = tenant
	}

	return &out
}
