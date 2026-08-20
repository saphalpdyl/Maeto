package dataplane

import (
	"context"
	"net/netip"
)

// Bare-minimum interface for data plane. LinuxNetlink is the default;
// LinuxShell is the original iproute2 implementation kept for comparison.
type Dataplane interface {
	AddVRF(ctx context.Context, tableName string, tableId int) error
	InsertXFRMInterface(ctx context.Context, interfaceName string, underLayIface string, ifID uint32, vrfTableName string) error
	InsertReturnPrefix(ctx context.Context, tunnelIface string, vrfTableName string, prefix netip.Prefix) error
	UpsertPolicy(ctx context.Context, nhid, dtsid, vrfTableId, vrfTableName string, sids []string) error
	UpsertRouteToPolicy(ctx context.Context, dest, vrfTableId, nhid string) error
	GetDefaultRouteAndDev(ctx context.Context) (string, string, error)
}
