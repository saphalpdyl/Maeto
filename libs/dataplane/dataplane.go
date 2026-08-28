package dataplane

import (
	"net"
	"net/netip"
)

type Via struct {
	AddrFamily int
	Addr       net.IP
}

type DataplaneRoute struct {
	// Dev is the resolved name of LinkIndex. Desired state can only ever speak
	// names, so the read side translates -- the reconciler has no way to.
	Dev        string
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

type EncapType string

const (
	EncapTypeDT46 EncapType = "dt46"
	EncapTypeB6   EncapType = "b6"
	EncapTypeDT4  EncapType = "dt4"
	EncapTypeDT6  EncapType = "dt6"
)

type DataplaneSID struct {
	Dev        string
	Dst        *net.IPNet
	Family     int
	Table      int
	Type       int
	VrfTableID int
	EncapType  EncapType
}

// DataplaneRule is a policy routing rule: "traffic matching Src looks up Table".
// It is what keeps the tunnel default out of the main table, so whatever owns
// the wan (dhcp, ra, pppd) is never fought over.
type DataplaneRule struct {
	Priority int
	Src      netip.Prefix
	Table    int
}

// Links represents VRF tables and XFRM interfaces for current implementation
type DataplaneLink interface {
	Type() string
	Attrs() *DataplaneLinkAttrs
}

type DataplaneLinkAttrs struct {
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
	ParentIndex  int
	MasterIndex  int
	// resolved forms of the two indexes above
	ParentName string
	MasterName string
}

type DataplaneVRF struct {
	DataplaneLinkAttrs
	TableID uint32
}

func (d *DataplaneVRF) Type() string               { return "vrf" }
func (d *DataplaneVRF) Attrs() *DataplaneLinkAttrs { return &d.DataplaneLinkAttrs }

type DataplaneXFRM struct {
	DataplaneLinkAttrs
	IfID uint32
}

func (d *DataplaneXFRM) Type() string               { return "xfrm" }
func (d *DataplaneXFRM) Attrs() *DataplaneLinkAttrs { return &d.DataplaneLinkAttrs }

var _ DataplaneLink = (*DataplaneVRF)(nil)
var _ DataplaneLink = (*DataplaneXFRM)(nil)

// Bare-minimum interface for data plane. LinuxNetlink is the default;
// LinuxShell is the original iproute2 implementation kept for comparison.
type Dataplane interface {
	UpsertVRF(tableName string, tableID int) error
	// a vrf is removed as a device, so this is a link index, not a table id
	RemoveVRF(linkIndex int) error

	// Works for both VRFs and XFRM interfaces
	GetLinksByType(ifaceType string) ([]DataplaneLink, error)

	// masterVRFIndex == nil means no vrf master, which is the cpe case
	InsertXFRMInterface(interfaceName string, underLayIface string, ifID uint32, masterVRFName *string) error
	RemoveXFRMInterface(linkIndex int) error

	// tableID names the table directly; unix.RT_TABLE_MAIN on a cpe
	UpsertRule(priority int, src netip.Prefix, tableID int) error
	RemoveRule(priority int, src netip.Prefix, tableID int) error
	GetRules() ([]DataplaneRule, error)

	// TODO: Rename tunnelIface to just dev
	InsertPrefixRoute(tunnelIface string, tableID int, prefix netip.Prefix, via netip.Addr) error
	RemovePrefixRoute(prefix netip.Prefix, tableID int) error
	GetPrefixRoutes() ([]DataplaneRoute, error)

	UpsertPolicy(nhid, dtsid, vrfTableId, vrfTableName string, sids []string) error
	UpsertRouteToPolicy(dest, vrfTableId, nhid string) error

	UpsertDT46SID(sid netip.Addr, vrfTableID int) error
	RemoveSID(sid netip.Addr) error
	GetSIDs() ([]DataplaneSID, error)

	GetDefaultRouteAndDev() (string, string, error)
}
