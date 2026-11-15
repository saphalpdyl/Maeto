package controlapi

const SubjectPortalAuthIdentity = "maeto.control.portal.auth.identity"
const SubjectHealthReady = "maeto.control.health.ready"
const SubjectPETunnelUpdate = "maeto.control.report.pe.tunnel_update"
const SubjectCPETunnelUpdate = "maeto.control.report.cpe.tunnel_update"
const SubjectControllerPETunnelResync = "maeto.control.request.pe.tunnel_resync"
const SubjectControllerCPETunnelResync = "maeto.control.request.cpe.tunnel_resync"

type PortalAuthEndpointRequest struct {
	PortalID string `json:"portal_id"`
}

type PortalAuthEndpointResponse struct {
	LocalSwanIdentity string                  `json:"local_swan_identity"`
	Prefix            string                  `json:"prefix"`
	Attachments       map[string]PEAttachment `json:"attachments"`
}

// PEAttachment provides the portal with the info necessary to initiate a IPSec tunnel multiple PEs
type PEAttachment struct {
	NodeID             string `json:"node_id"`
	AttachAddr         string `json:"attach_addr"`
	RemoteSwanIdentity string `json:"remote_swan_identity"`
}

type PETunnelUpdateRequest struct {
	PortalID string `json:"portal_id"`
	IfID     uint32 `json:"if_id"`
	NodeID   string `json:"node_id"`
}

type CPETunnelUpdateRequest struct {
	PortalID string            `json:"portal_id"`
	IfIds    map[string]uint32 `json:"if_ids"`
}

type TunnelUpdateResponse struct {
	Ok bool `json:"ok"`
}
