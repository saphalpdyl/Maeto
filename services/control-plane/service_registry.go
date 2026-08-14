// Holds information about desired FIB states
package controlplane

import (
	"encoding/json"
	"net/netip"
	"sync"
)

type ServiceRegistryConfig struct {
}

type ServiceRegistry struct {
	config *ServiceRegistryConfig

	mu      sync.RWMutex //nolint:unused // guards desired/reports once concurrent access lands
	desired map[NodeID]NodeIntent
	reports map[NodeID]NodeReport
}

func NewServiceRegistry(config *ServiceRegistryConfig) *ServiceRegistry {
	return &ServiceRegistry{
		config:  config,
		desired: map[NodeID]NodeIntent{},
		reports: map[NodeID]NodeReport{},
	}
}

type NodeIntent struct {
	Node     NodeID
	Gen      int32
	Snapshot bool
	Hash     string

	VRFs     []VRFSpec
	Actions  []ActionSpec // Actions installed by the PCE, End.X actions come from NodeReport
	Nexthops []NexthopSpec
	Routes   []RouteSpec
}

type VRFSpec struct {
	ID              string
	TableName       string
	SlavedInterface *string
}

type ActionSpec struct {
	SID           netip.Addr
	Action        string
	ActionOptions json.RawMessage
}

type NexthopSpec struct {
	ID       uint32
	Segments []netip.Addr
}

type RouteSpec struct {
	VRF     *VRFSpec
	Prefix  netip.Prefix
	Nexthop *NexthopSpec
}

type NodeReport struct {
	Node NodeID
	Seq  uint64

	Locator netip.Prefix

	SystemID string
	AdjSIDs  []AdjacencySID
}

type AdjacencySID struct {
	SID          netip.Addr
	PeerSystemID string
}
