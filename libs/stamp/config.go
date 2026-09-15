package stamp

type ErrorEstimateConfig struct {
	Scale        uint8
	Multiplier   uint8
	ClockFormat  TimestampClockFormat
	Synchronized bool
}

type Config struct {
	ErrorEstimate ErrorEstimateConfig

	// Bind the underlying fd to a net device
	// Used for Route-based SR implementations in Linux
	BindToDev *string // usually probe-vrf
}
