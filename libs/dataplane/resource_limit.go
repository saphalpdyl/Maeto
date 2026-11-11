package dataplane

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Limit polices traffic arriving on an interface, dropping what exceeds the rate.
// Linux implementation uses nftables rules to enforce this.
// A nil Mark matches every mark not in ExceptMarks.
type Limit struct {
	Iifname     string
	Mark        *uint32
	ExceptMarks []uint32
	RateBytes   uint64
	BurstBytes  uint32

	Handle uint64 // observed kernel-assigned value
}

func (l Limit) ID() ID {
	mark := "any"
	if l.Mark != nil {
		mark = strconv.FormatUint(uint64(*l.Mark), 10)
	}

	except := slices.Clone(l.ExceptMarks)
	slices.Sort(except)

	exceptStrs := make([]string, len(except))
	for i, m := range except {
		exceptStrs[i] = strconv.FormatUint(uint64(m), 10)
	}

	return ID{
		Kind: KindLimit,
		Key: fmt.Sprintf("%s:%s.%s.%s.%d.%d",
			KindLimit, l.Iifname, mark, strings.Join(exceptStrs, ","), l.RateBytes, l.BurstBytes),
	}
}

func (l Limit) CompareTo(actual Resource) Action {
	return ActionNone
}

func (l Limit) Type() string {
	return "limit"
}
