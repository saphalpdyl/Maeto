package maetoagent

import "context"

// Bare-minimum interface for data plane.
// Initial versions will use linux kernel as the dataplane by shelling out
type Dataplane interface {
	AddVRF(ctx context.Context, tableName, tableId, iface string) error
	UpsertPolicy(ctx context.Context, nhid, dtsid, vrfTableId, vrfTableName string, sids []string) error
	UpsertRouteToPolicy(ctx context.Context, dest, vrfTableId, nhid string) error
	GetDefaultRouteAndDev(ctx context.Context) (string, string, error)
}
