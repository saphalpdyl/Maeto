package maetoagent

import "context"

// Bare-minimum interface for data plane.
// Initial versions will use linux kernel as the dataplane by shelling out
type Dataplane interface {
	AddVRF(ctx context.Context, tableName string, tableId int) error
	InsertXFRMInterface(ctx context.Context, interfaceName string, underLayIface string, ifID uint32, vrfTableName string) error
	InsertReturnPrefix(ctx context.Context, tunnelIface string, vrfTableName string, prefix string) error
	UpsertPolicy(ctx context.Context, nhid, dtsid, vrfTableId, vrfTableName string, sids []string) error
	UpsertRouteToPolicy(ctx context.Context, dest, vrfTableId, nhid string) error
	GetDefaultRouteAndDev(ctx context.Context) (string, string, error)
}
