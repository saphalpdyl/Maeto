package dataplane

import (
	"fmt"
	"net/netip"
)

// A cpe carries one tenant, so one table and one rule priority is enough. Both
// are maeto's alone: main stays owned by whatever manages the wan.
const (
	CPETunnelTableBase      = 100
	CPERulePriority         = 100
	CPEFailoverRulePriority = 99
	CPESiteRulePriority     = 98
)

// The kernel installs an ipv6 route given no metric at 1024 and reports it
// back as 1024, so desired 0 and observed 1024 are the same route
const DefaultRouteMetric = 1024

func routeMetric(metric int) int {
	if metric <= 0 {
		return DefaultRouteMetric
	}

	return metric
}

// Ideompotently replaced. no need for delete+create
type Route struct {
	Table     int          `rc:"id"`
	Dst       netip.Prefix `rc:"id"`
	Dev       string       `rc:"mut"`
	Via       netip.Addr   `rc:"mut"`
	Metric    int
	AllowLeak bool
}

func (r *Route) ID() ID {
	return ID{
		Kind: KindRoute,
		Key:  fmt.Sprintf("%s:%d.%s.%s.%d", KindRoute, r.Table, r.Dst.String(), r.Dev, routeMetric(r.Metric)),
	}
}

func (r *Route) Type() string { return "route" }

func (r *Route) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}
	if a, ok := actual.(*Route); ok {
		if r.Table != a.Table || r.Dst != a.Dst || r.Dev != a.Dev || r.Via != a.Via || routeMetric(r.Metric) != routeMetric(a.Metric) {
			return ActionReplace
		}
		return ActionNone
	}
	return ActionReplace
}
