// Holds information about desired FIB states
package controlplane

import (
	"sync"
)

type ServiceRegistryConfig struct {
}

type ServiceRegistry struct {
	config *ServiceRegistryConfig

	mu sync.RWMutex //nolint:unused // guards desired state once it lands
}

func NewServiceRegistry(config *ServiceRegistryConfig) *ServiceRegistry {
	return &ServiceRegistry{
		config: config,
	}
}
