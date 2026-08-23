package log

import (
	"log/slog"
)

type LogDomain string

const (
	DomainControlPlaneLifecycle LogDomain = "CONTROL_PLANE_LIFECYCLE"
	DomainConfig                LogDomain = "CONFIG"
	DomainServiceRegistry       LogDomain = "SERVICE_REGISTRY"
)

func Domain(v LogDomain) slog.Attr { return slog.String("domain", string(v)) }

func InstanceID(v string) slog.Attr { return slog.String("service.instance.id", v) }

func Operation(v string) slog.Attr  { return slog.String("operation", v) }
func DurationMs(ms int64) slog.Attr { return slog.Int64("duration_ms", ms) }
func Attempt(n int) slog.Attr       { return slog.Int("attempt", n) }

func EnvKey(v string) slog.Attr   { return slog.String("env.key", v) }
func EnvValue(v string) slog.Attr { return slog.String("env.value", v) }
func EnvKeys(v []string) slog.Attr {
	return slog.Any("env.keys", v)
}

func ListenAddress(v string) slog.Attr { return slog.String("listen_address", v) }
func NodeID(v string) slog.Attr        { return slog.String("node.id", v) }

func Err(err error) slog.Attr {
	if err == nil {
		return slog.String("error", "<nil>")
	}
	return slog.String("error", err.Error())
}
