package controlplane

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	log "github.com/saphalpdyl/maeto/services/control-plane/log"
)

type Config struct {
	OtelSink     string
	OtelEndpoint string

	ListenAddress string

	NatsConnectURL string
	DatabaseURL    string

	StatePath string
	DataDir   string

	AgentHeartbeatInterval time.Duration

	// DefaultedKeys lists the environment variables that were not set and
	// therefore fell back to their compiled-in default.
	DefaultedKeys []string
}

const (
	DefaultOtelSink               = "stdout"
	DefaultOtelEndpoint           = "otel-collector:4318"
	DefaultListenAddress          = ":8080"
	DefaultNatsConnectURL         = "nats://nats:4222"
	DefaultDatabaseURL            = "postgres://postgres:admin@db:5432/postgres?sslmode=disable"
	DefaultStatePath              = "/app/state/latest.json"
	DefaultDataDir                = "/app/data"
	DefaultAgentHeartbeatInterval = 30 * time.Second
)

// resolver reads environment variables and remembers which ones fell back to a
// default so GetConfig can report them in one place.
type resolver struct {
	logger    *slog.Logger
	defaulted []string
	failed    bool
}

func (r *resolver) str(k, def string, secret bool) string {
	val := strings.TrimSpace(os.Getenv(k))
	if val != "" {
		return val
	}

	shown := def
	if secret {
		shown = "<redacted>"
	}
	r.warnDefault(k, shown)

	return def
}

func (r *resolver) duration(k string, def time.Duration) time.Duration {
	val := strings.TrimSpace(os.Getenv(k))
	if val == "" {
		r.warnDefault(k, def.String())
		return def
	}

	d, err := time.ParseDuration(val)
	if err != nil {
		// Accept a bare number of seconds too, matching argus' *_SECONDS vars.
		secs, secErr := strconv.Atoi(val)
		if secErr != nil {
			r.fail(k, val, fmt.Errorf("not a duration (%q) or a number of seconds", val))
			return def
		}
		d = time.Duration(secs) * time.Second
	}

	if d <= 0 {
		r.fail(k, val, fmt.Errorf("must be greater than zero"))
		return def
	}

	return d
}

func (r *resolver) warnDefault(k, shown string) {
	r.logger.Warn("environment variable unset, using DEFAULT value", log.EnvKey(k), log.EnvValue(shown))
	r.defaulted = append(r.defaulted, k)
}

func (r *resolver) fail(k, val string, err error) {
	r.logger.Error("invalid environment variable", log.EnvKey(k), log.EnvValue(val), log.Err(err))
	r.failed = true
}

func GetConfig() Config {
	r := resolver{logger: slog.Default().With(log.Domain(log.DomainConfig))}

	cfg := Config{
		OtelSink:     r.str("OTEL_SINK", DefaultOtelSink, false),
		OtelEndpoint: r.str("OTEL_ENDPOINT", DefaultOtelEndpoint, false),

		ListenAddress: r.str("CONTROL_PLANE_LISTEN_ADDRESS", DefaultListenAddress, false),

		NatsConnectURL: r.str("NATS_CONNECT_URL", DefaultNatsConnectURL, false),
		DatabaseURL:    r.str("MAETO_DB_URL", DefaultDatabaseURL, true),

		StatePath: r.str("CONTROL_PLANE_STATE_DIR", DefaultStatePath, false),
		DataDir:   r.str("CONTROL_PLANE_DATA_DIR", DefaultDataDir, false),

		AgentHeartbeatInterval: r.duration("AGENT_HEARTBEAT_INTERVAL", DefaultAgentHeartbeatInterval),
	}

	cfg.DefaultedKeys = r.defaulted

	if r.failed {
		os.Exit(1)
	}

	if len(cfg.DefaultedKeys) > 0 {
		r.logger.Warn(
			"control plane started with DEFAULT configuration values",
			slog.Int("count", len(cfg.DefaultedKeys)),
			log.EnvKeys(cfg.DefaultedKeys),
		)
	}

	return cfg
}
