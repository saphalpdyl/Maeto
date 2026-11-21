package dataplane

import (
	"fmt"
	"net/netip"
)

// need for delete + create
type XFRM struct {
	Name          string `rc:"id"`
	IfID          uint32 `rc:"id"`
	Parent        string
	MasterVRF     string `rc:"mut"`
	LinkLocalAddr netip.Prefix
	Index         int // observed only
}

func (x *XFRM) ID() ID {
	return ID{
		Kind: KindXFRM,
		Key:  fmt.Sprintf("%s:%s.%d.%s", KindXFRM, x.Name, x.IfID, x.LinkLocalAddr.String()),
	}
}

func (x *XFRM) Type() string { return "xfrm" }

func (x *XFRM) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}
	if a, ok := actual.(*XFRM); ok {
		if x.Name != a.Name || x.IfID != a.IfID || x.Parent != a.Parent || x.MasterVRF != a.MasterVRF {
			return ActionReplace
		}
		return ActionNone
	}
	return ActionReplace
}
