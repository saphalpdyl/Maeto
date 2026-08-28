package dataplane

import "net/netip"

// High-level intent instructions that get converted to dataplane instructions
// by the reconciler
type CPEIntent struct {
	PortalID             string       `json:"portal_id"`
	TunnelInterfaceID    uint32       `json:"tunnel_interface_id"`
	TunnelPE             string       `json:"tunnel_pe"`
	TunnelPEEndpointAddr netip.Addr   `json:"tunnel_pe_endpoint_addr"`
	TenantID             string       `json:"tenant_id"`
	TenantPrefix         netip.Prefix `json:"tenant_prefix"`
	SitePrefix           netip.Prefix `json:"site_prefix"`
}

func (*CPEIntent) isIntent() {}

// every field is a value type, so the struct copy is the deep copy
func (i *CPEIntent) Clone() Intent {
	if i == nil {
		return nil
	}

	out := *i

	return &out
}
