package log

import (
	"log/slog"
)

type LogDomain string

const (
	DomainAgentLifecycle LogDomain = "AGENT_LIFECYCLE"
	DomainControlPlane   LogDomain = "CONTROL_PLANE"
	DomainDataplane      LogDomain = "DATAPLANE"
)

func Domain(v LogDomain) slog.Attr { return slog.String("domain", string(v)) }

func InstanceID(v string) slog.Attr { return slog.String("service.instance.id", v) }
func PopID(v string) slog.Attr      { return slog.String("pop.id", v) }
func NodeName(v string) slog.Attr   { return slog.String("node.name", v) }
func Locator(v string) slog.Attr    { return slog.String("locator", v) }
func Subject(v string) slog.Attr    { return slog.String("subject", v) }
func Interface(v string) slog.Attr  { return slog.String("interface", v) }
func VRF(v string) slog.Attr        { return slog.String("vrf", v) }

func Operation(v string) slog.Attr  { return slog.String("operation", v) }
func DurationMs(ms int64) slog.Attr { return slog.Int64("duration_ms", ms) }
func Attempt(n int) slog.Attr       { return slog.Int("attempt", n) }

func Err(err error) slog.Attr {
	if err == nil {
		return slog.String("error", "<nil>")
	}

	return slog.String("error", err.Error())
}
