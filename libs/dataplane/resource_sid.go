package dataplane

import (
	"fmt"
	"net/netip"
)

type SIDType string

const (
	SIDDT46 SIDType = "dt46" // Ranges from locator:node:(f001->ffff)::
	SIDDTB  SIDType = "dtb"  // unused
)

// SID is basically a special route
type SID struct {
	SIDType SIDType
	SID     netip.Addr
	Table   int
	Metric  int
}

func (s *SID) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}

	if a, ok := actual.(*SID); ok {
		if (s.SIDType != a.SIDType) || (s.SID != a.SID) {
			return ActionReplace
		}

		return ActionNone
	}

	return ActionReplace
}

func (s *SID) ID() ID {
	return ID{
		Kind: KindSID,
		Key:  fmt.Sprintf("%s.%s", s.SIDType, s.SID.String()),
	}
}

func (s *SID) Type() string {
	return "sid"
}
