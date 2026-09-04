package dataplane

import (
	"net/netip"
	"slices"
)

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
	Tenants map[string]*TenantIntent `json:"tenants"`
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

	// SID paths to install for a given prefix
	// For example: fd7a:db8:beef::/64 -> fc00:0:1::, fc00:0:2::, fc00:0:3::, fc00:0:d46:2183:2341 (End.DT46 SID)
	InstallPaths []PESIDInstallIntent `json:"install_paths"`
}

type PESIDInstallIntent struct {
	TenantID string `json:"tenant_id"`
	// for which to route towards
	// TODO: In the future, there will be multiple subnets that will consume this
	PrefixRoutes []netip.Prefix `json:"prefix_route"`
	Segments     []netip.Addr   `json:"segments"`
	// TODO: SRv6 SR-Policy metric; will be used in the future
	Color uint64 `json:"color"`
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
	out.Tenants = make(map[string]*TenantIntent, len(i.Tenants))
	for tenantID, tenant := range i.Tenants {
		if tenant == nil {
			out.Tenants[tenantID] = nil
			continue
		}

		copied := &TenantIntent{
			DT46SID:       tenant.DT46SID,
			PortalIntents: make(map[string]PE_PortalIntent, len(tenant.PortalIntents)),
		}

		for portalID, portal := range tenant.PortalIntents {
			copied.PortalIntents[portalID] = portal // PE_PortalIntent is all value types
		}

		if tenant.InstallPaths != nil {
			copied.InstallPaths = make([]PESIDInstallIntent, len(tenant.InstallPaths))
			for idx, install := range tenant.InstallPaths {
				install.PrefixRoutes = slices.Clone(install.PrefixRoutes)
				install.Segments = slices.Clone(install.Segments)
				copied.InstallPaths[idx] = install
			}
		}

		out.Tenants[tenantID] = copied
	}

	return &out
}
