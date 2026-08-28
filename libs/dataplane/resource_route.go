package dataplane

import (
	"fmt"
	"net/netip"
)

// A cpe carries one tenant, so one table and one rule priority is enough. Both
// are maeto's alone: main stays owned by whatever manages the wan.
const (
	CPETunnelTable  = 100
	CPERulePriority = 100
)

// Ideompotently replaced. no need for delete+create
type Route struct {
	Table  int          `rc:"id"`
	Dst    netip.Prefix `rc:"id"`
	Dev    string       `rc:"mut"`
	Via    netip.Addr   `rc:"mut"`
	Metric int
}

func (r *Route) ID() ID {
	return ID{
		Kind: KindRoute,
		Key:  fmt.Sprintf("%d.%s.%s", r.Table, r.Dst.String(), r.Dev),
	}
}

func (r *Route) Type() string { return "route" }

func (r *Route) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}
	if a, ok := actual.(*Route); ok {
		if r.Table != a.Table || r.Dst != a.Dst || r.Dev != a.Dev || r.Via != a.Via || r.Metric != a.Metric {
			return ActionReplace
		}
		return ActionNone
	}
	return ActionReplace
}
