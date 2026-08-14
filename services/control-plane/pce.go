package controlplane

import (
	"context"

	"github.com/nats-io/nats.go"
)

type PCE struct {
	costGraph       *CostGraph
	serviceRegistry *ServiceRegistry
	topo            *ClabTopologyManager

	nats *nats.Conn
}

type PCEConfig struct {
	StatePath       string
	TopologyDirPath string
}

func NewPCE(
	nats *nats.Conn,
	cfg PCEConfig,
) *PCE {
	return &PCE{
		costGraph:       NewCostGraph(),
		serviceRegistry: NewServiceRegistry(&ServiceRegistryConfig{}),
		topo: NewClabTopologyManager(ClabTopologyConfig{ //nolint:staticcheck // keep configs decoupled; they only coincidentally match
			StatePath:       cfg.StatePath,
			TopologyDirPath: cfg.TopologyDirPath,
		}),

		nats: nats,
	}
}

func (p *PCE) Run(ctx context.Context) {

}
