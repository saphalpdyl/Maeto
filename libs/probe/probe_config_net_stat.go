package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"
)

// ProbeConfigNetStat spawns an agent that reads to multiple sys/class/net/<dev>/statistics/**
type ProbeConfigNetStat struct {
	Devs      []string `json:"devs"`
	StatItems []string `json:"stat_items"` // F.e. rx_bytes, rx_errors , tx_packets etc.

	ProbeInterval time.Duration `json:"probe_interval"`
}

func (p ProbeConfigNetStat) GetID() string {
	devs := slices.Sorted(slices.Values(p.Devs))
	stats := slices.Sorted(slices.Values(p.StatItems))
	devs = slices.Compact(devs)
	stats = slices.Compact(stats)

	var b strings.Builder
	b.WriteString(strings.Join(devs, "\x00"))
	b.WriteString("\x01")
	b.WriteString(strings.Join(stats, "\x00"))

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}
