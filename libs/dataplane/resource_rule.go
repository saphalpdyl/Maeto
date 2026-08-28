package dataplane

import (
	"fmt"
	"net/netip"
)

// need for delete + create: a rule has no mutable field, every one identifies it
type Rule struct {
	Priority int          `rc:"id"`
	Src      netip.Prefix `rc:"id"`
	Table    int          `rc:"id"`
}

func (r *Rule) ID() ID {
	return ID{
		Kind: KindRule,
		Key:  fmt.Sprintf("%d.%s.%d", r.Priority, r.Src.String(), r.Table),
	}
}

func (r *Rule) Type() string { return "rule" }

func (r *Rule) CompareTo(actual Resource) Action {
	a, ok := actual.(*Rule)
	if !ok {
		return ActionReplace
	}
	if r.Priority != a.Priority || r.Src != a.Src || r.Table != a.Table {
		return ActionReplace
	}

	return ActionNone
}
