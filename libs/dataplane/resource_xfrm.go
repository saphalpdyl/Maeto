package dataplane

import "fmt"

// need for delete + create
type XFRM struct {
	Name      string `rc:"id"`
	IfID      uint32 `rc:"id"`
	Parent    string
	MasterVRF string `rc:"mut"`
	Index     int    // observed only
}

func (x *XFRM) ID() ID {
	return ID{
		Kind: KindXFRM,
		Key:  fmt.Sprintf("%s.%d", x.Name, x.IfID),
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
