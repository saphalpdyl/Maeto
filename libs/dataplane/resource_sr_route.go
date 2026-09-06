package dataplane

import (
	"fmt"
	"net/netip"
	"strings"
)

type SRRoute struct {
	Prefix   netip.Prefix
	Segments []netip.Addr
	Table    int
	Color    int
}

func (s *SRRoute) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}

	if a, ok := actual.(*SRRoute); ok {
		if s.Color != a.Color {
			return ActionReplace
		}

		if a.Table != s.Table {
			return ActionReplace
		}

		if (len(s.Segments) != len(a.Segments)) || (a.Color != s.Color) {
			return ActionReplace
		}

		for i, actualSegment := range a.Segments {
			if s.Segments[i] != actualSegment {
				return ActionReplace
			}
		}

		return ActionNone
	}

	return ActionReplace
}

func (s *SRRoute) ID() ID {
	var segmentStr strings.Builder
	for _, segment := range s.Segments {
		segmentStr.WriteString(segment.String())
	}

	return ID{
		Kind: KindSRRoute,
		Key: fmt.Sprintf(
			"%s.%s.%s.%s",
			fmt.Sprint(s.Table),
			s.Prefix.String(),
			segmentStr.String(),
			fmt.Sprint(s.Color),
		),
	}
}

// Type implements [Resource].
func (s *SRRoute) Type() string {
	return "sr_route"
}
