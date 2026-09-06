package dataplane

// The shared neutral that PEIntent, CPEIntent and the FIB state convert to before
// diffing.
type Resource interface {
	ID() ID
	// only the concrete type knows which of its fields can changplace
	CompareTo(actual Resource) Action // ActionNone | ActionUpdatActionReplace
	Type() string
}

type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionReplace Action = "replace"
	ActionDelete  Action = "delete"
	ActionNone    Action = "none"
)

type Kind string

const (
	KindVRF     Kind = "vrf"
	KindRule    Kind = "rule"
	KindXFRM    Kind = "xfrm"
	KindRoute   Kind = "route"
	KindSID     Kind = "sid"
	KindSRRoute Kind = "sr_route"
)

type ID struct {
	Kind Kind
	Key  string // "vrf-tenant-231" | "maeto-tun0" | "254|::/0"
}
