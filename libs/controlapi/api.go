package controlapi

const SubjectPortalAuthIdentity = "maeto.control.portal.auth.identity"
const SubjectHealthReady = "maeto.control.health.ready"

type PortalAuthEndpointRequest struct {
	PortalID string `json:"portal_id"`
}

type PortalAuthEndpointResponse struct {
	LocalSwanIdentity  string `json:"local_swan_identity"`
	RemoteSwanIdentity string `json:"remote_swan_identity"`
	AttachNode         string `json:"attach_node"`
	Prefix             string `json:"prefix"`
	AttachNodeAddr     string `json:"attach_node_addr"`
}
