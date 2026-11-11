package dataplane

// The shared neutral that PEIntent, CPEIntent and the FIB state convert to before
// diffing.
type Resource interface {
	ID() ID
	// only the concrete type knows which of its fields can changplace
	CompareTo(actual Resource) Action // ActionNone | ActionUpdatActionReplace
	Type() string
}

// TODO: Make use of CompareTo() for more advanced (and efficient) resource allocation
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
	KindVRF            Kind = "vrf"
	KindRule           Kind = "rule"
	KindXFRM           Kind = "xfrm"
	KindRoute          Kind = "route"
	KindSID            Kind = "sid"
	KindSRRoute        Kind = "sr_route"
	KindClassification Kind = "classification"
	KindLimit          Kind = "limit"
)

type ID struct {
	Kind Kind
	Key  string // "vrf-tenant-231" | "maeto-tun0" | "254|::/0"
}
