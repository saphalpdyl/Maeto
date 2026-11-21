package dataplane

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Special resource; not applied to the dataplane, but stored as a state
// trigger by the trigger function ActivateRepair() ResetRepair() etc.
type PLRTrigger struct {
	Egress   string
	Prefixes []netip.Prefix
}

func (p PLRTrigger) ID() ID {
	prefixes := make([]string, 0, len(p.Prefixes))
	for _, prefix := range p.Prefixes {
		prefixes = append(prefixes, prefix.String())
	}

	slices.Sort(prefixes)
	return ID{
		Kind: KindPLRTrigger,
		Key:  fmt.Sprintf("plr_trigger:%s.%s", p.Egress, strings.Join(prefixes, "-")),
	}
}

func (p PLRTrigger) CompareTo(actual Resource) Action {
	return ActionNone
}

func (p PLRTrigger) Type() string {
	return "plr_trigger"
}
