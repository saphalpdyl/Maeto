package maetoportal

import (
	"log/slog"
	"os"
	"time"

	"github.com/saphalpdyl/maeto/libs/common"
)

type PortalConfig struct {
	OtelSink     string
	OtelEndpoint string

	NatsConnectURL string

	PortalID  string
	AttachPop string

	ShutdownGracePeriod time.Duration
}

func handleConfigErrorWithExit(err error) {
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func getFromEnvOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func GetConfig() PortalConfig {
	otelSink, err := common.TryGetFromEnv("OTEL_SINK")
	handleConfigErrorWithExit(err)

	otelEndpoint, err := common.TryGetFromEnv("OTEL_ENDPOINT")
	handleConfigErrorWithExit(err)

	natsConnectUrl, err := common.TryGetFromEnv("NATS_CONNECT_URL")
	handleConfigErrorWithExit(err)

	portalId, err := common.TryGetFromEnv("PORTAL_ID")
	handleConfigErrorWithExit(err)

	attachPop, err := common.TryGetFromEnv("PORTAL_ATTACH_POP")
	handleConfigErrorWithExit(err)

	gracePeriod, err := time.ParseDuration(getFromEnvOr("PORTAL_SHUTDOWN_GRACE_PERIOD", "15s"))
	handleConfigErrorWithExit(err)

	return PortalConfig{
		OtelSink:            otelSink,
		OtelEndpoint:        otelEndpoint,
		NatsConnectURL:      natsConnectUrl,
		PortalID:            portalId,
		AttachPop:           attachPop,
		ShutdownGracePeriod: gracePeriod,
	}
}
