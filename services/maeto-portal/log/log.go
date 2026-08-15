package log

import (
	"log/slog"
)

type LogDomain string

const (
	DomainPortalLifecycle LogDomain = "PORTAL_LIFECYCLE"
	DomainControlPlane    LogDomain = "CONTROL_PLANE"
	DomainTunnel          LogDomain = "TUNNEL"
)

func Domain(v LogDomain) slog.Attr { return slog.String("domain", string(v)) }

func InstanceID(v string) slog.Attr { return slog.String("service.instance.id", v) }
func PortalID(v string) slog.Attr   { return slog.String("portal.id", v) }
func PopID(v string) slog.Attr      { return slog.String("pop.id", v) }

func Operation(v string) slog.Attr  { return slog.String("operation", v) }
func DurationMs(ms int64) slog.Attr { return slog.Int64("duration_ms", ms) }
func Attempt(n int) slog.Attr       { return slog.Int("attempt", n) }

func Err(err error) slog.Attr {
	if err == nil {
		return slog.String("error", "<nil>")
	}
	return slog.String("error", err.Error())
}
