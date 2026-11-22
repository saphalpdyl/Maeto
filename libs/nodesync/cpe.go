package nodesync

import (
	"net/netip"
	"slices"
)

// High-level intent instructions that get converted to dataplane instructions
// by the reconciler
type CPEIntent struct {
	PortalID     string            `json:"portal_id"`
	TenantID     string            `json:"tenant_id"`
	TenantPrefix netip.Prefix      `json:"tenant_prefix"`
	SitePrefix   netip.Prefix      `json:"site_prefix"`
	Tunnels      []CPETunnelIntent `json:"tunnels"`

	DownNodes []string `json:"down_nodes"`
}

type TunnelRole string

const (
	TunnelRolePrimary TunnelRole = "primary"
	TunnelRoleStandby TunnelRole = "standby"
)

type CPETunnelIntent struct {
	TunnelInterfaceID    uint32     `json:"tunnel_interface_id"`
	TunnelPE             string     `json:"tunnel_pe"`
	TunnelPEEndpointAddr netip.Addr `json:"tunnel_pe_endpoint_addr"`
	Role                 TunnelRole `json:"tunnel_role"`
}

func (*CPEIntent) isIntent() {}

func (i *CPEIntent) Clone() Intent {
	if i == nil {
		return nil
	}

	out := *i
	out.Tunnels = slices.Clone(i.Tunnels)
	out.DownNodes = slices.Clone(i.DownNodes)

	return &out
}
