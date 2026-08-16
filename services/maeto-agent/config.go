package maetoagent

import "os"

const (
	DefaultNodePath       = "/etc/maeto/node.json"
	DefaultNatsConnectURL = "nats://clab-eight-pop-nats:4222"
	DefaultOtelSink       = "stdout"
	DefaultOtelEndpoint   = "otel-collector:4318"
)

type Config struct {
	OtelSink     string
	OtelEndpoint string

	NatsConnectURL string
	NodePath       string
}

func GetConfig() Config {
	return Config{
		OtelSink:     getFromEnvOr("OTEL_SINK", DefaultOtelSink),
		OtelEndpoint: getFromEnvOr("OTEL_ENDPOINT", DefaultOtelEndpoint),

		NatsConnectURL: getFromEnvOr("NATS_CONNECT_URL", DefaultNatsConnectURL),
		NodePath:       getFromEnvOr("MAETO_NODE_FILE", DefaultNodePath),
	}
}

func getFromEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
