package dataplane

// need for delete + create
type VRF struct {
	Name    string `rc:"id"`
	TableID uint32 `rc:"id"`
	Index   int    // observed only
}

func (v *VRF) ID() ID {
	return ID{
		Kind: KindVRF,
		Key:  v.Name,
	}
}

func (v *VRF) Type() string { return "vrf" }

func (v *VRF) CompareTo(actual Resource) Action {
	if actual == nil {
		return ActionCreate
	}
	if a, ok := actual.(*VRF); ok {
		if v.Name != a.Name || v.TableID != a.TableID {
			return ActionReplace
		}
		return ActionNone
	}
	return ActionReplace
}
