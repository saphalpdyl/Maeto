package controlplane

type Argus struct {
	chans map[string]chan struct{} // key is the Telemetry Key specifying a link
}

func NewArgus() *Argus {
	return &Argus{
		chans: make(map[string]chan struct{}),
	}
}
