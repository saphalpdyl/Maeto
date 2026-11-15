package dataplane

import (
	"fmt"
	"net/netip"
)

// need for delete + create: a rule has no mutable field, every one identifies it
type Rule struct {
	Priority int           `rc:"id"`
	Src      *netip.Prefix `rc:"id"`
	Dst      *netip.Prefix
	Table    int    `rc:"id"`
	Mark     int    `rc:"mark"`
	Iif      string `rc:"iifName"`
}

func (r *Rule) ID() ID {
	src := ""
	dst := ""

	if r.Src != nil {
		src = r.Src.String()
	}

	if r.Dst != nil {
		dst = r.Dst.String()
	}

	return ID{
		Kind: KindRule,
		Key:  fmt.Sprintf("%s:%d.%s.%s.%d.%d.%s", KindRule, r.Priority, src, dst, r.Table, r.Mark, r.Iif),
	}
}

func (r *Rule) Type() string { return "rule" }

func (r *Rule) CompareTo(actual Resource) Action {
	a, ok := actual.(*Rule)
	if !ok {
		return ActionReplace
	}
	if r.Priority != a.Priority || r.Table != a.Table || r.Mark != a.Mark || r.Iif != a.Iif ||
		!samePrefix(r.Src, a.Src) || !samePrefix(r.Dst, a.Dst) {
		return ActionReplace
	}

	return ActionNone
}

func samePrefix(a, b *netip.Prefix) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}
