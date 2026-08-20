package maetoportal

import (
	"context"
)

// Bare-minimum interface for data plane.
// Initial versions will use linux kernel as the dataplane by shelling out
type Dataplane interface {
	InsertXFRMInterface(ctx context.Context, interfaceName string, underLayIface string, ifID uint32, vrfTableName string) error
	GetDefaultRouteAndDev(ctx context.Context) (string, string, error)
	InstallRoute()
}
