package dataplane

import (
	"net"
	"net/netip"
)

type Via struct {
	AddrFamily int
	Addr       net.IP
}

// Categorizing Maeto installed routes should be done by implementations
//
// For example: netlink creates netlink.Route with .Proto == MaetoRouteProto [211]
// and netlink.Link with .LinkAttrs.Group == MaetoLinkGroup [211]
type DataplaneRoute struct {
	LinkIndex  int
	ILinkIndex int
	Dst        *net.IPNet
	Family     int
	Table      int
	Type       int
	Tos        int
	Via        Via
	Realm      int
	MTU        int
	Rtt        int
	AdvMSS     int
}

// Links represents VRF tables and XFRM interfaces for current implementation
type DataplaneLink struct {
	Index        int
	MTU          int
	TxQLen       int // Transmit Queue Length
	Name         string
	HardwareAddr net.HardwareAddr
	Flags        net.Flags
	RawFlags     uint32
	Alias        string
	EncapType    string
	PhysSwitchID int
	NumTxQueues  int
	NumRxQueues  int
	Group        uint32
	PermHWAddr   net.HardwareAddr
}

// Bare-minimum interface for data plane. LinuxNetlink is the default;
// LinuxShell is the original iproute2 implementation kept for comparison.
type Dataplane interface {
	UpsertVRF(tableName string, tableID int) error
	// a vrf is removed as a device, so this is a link index, not a table id
	RemoveVRF(linkIndex int) error

	// Works for both VRFs and XFRM interfaces
	GetLinksByType(ifaceType string) ([]DataplaneLink, error)

	// masterVRFIndex == nil means no vrf master, which is the cpe case
	InsertXFRMInterface(interfaceName string, underLayIface string, ifID uint32, masterVRFIndex *int) error
	RemoveXFRMInterface(linkIndex int) error

	// tableID names the table directly; unix.RT_TABLE_MAIN on a cpe
	InsertPrefixRoute(tunnelIface string, tableID int, prefix netip.Prefix) error
	RemovePrefixRoute(prefix netip.Prefix, tableID int) error
	GetPrefixRoutes() ([]DataplaneRoute, error)

	UpsertPolicy(nhid, dtsid, vrfTableId, vrfTableName string, sids []string) error
	UpsertRouteToPolicy(dest, vrfTableId, nhid string) error

	GetDefaultRouteAndDev() (string, string, error)
}
