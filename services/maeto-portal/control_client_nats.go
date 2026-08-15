package maetoportal

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
)

type NatsControlClient struct {
	url      string
	portalID string
	logger   *slog.Logger

	conn *nats.Conn
}

func NewNatsControlClient(url, portalID string, logger *slog.Logger) *NatsControlClient {
	return &NatsControlClient{
		url:      url,
		portalID: portalID,
		logger:   logger.With(log.Domain(log.DomainControlPlane)),
	}
}

func (c *NatsControlClient) Connect(ctx context.Context) error {
	opts := []nats.Option{
		nats.Name(fmt.Sprintf("maeto-portal/%s", c.portalID)),
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			c.logger.WarnContext(ctx, "control plane disconnected", log.Err(err))
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			c.logger.InfoContext(ctx, "control plane reconnected", slog.String("url", nc.ConnectedUrl()))
		}),
	}

	conn, err := nats.Connect(c.url, opts...)
	if err != nil {
		return fmt.Errorf("connect to nats at %s: %w", c.url, err)
	}

	c.conn = conn
	c.logger.InfoContext(ctx, "connected to control plane", slog.String("url", conn.ConnectedUrl()))

	return nil
}

func (c *NatsControlClient) Close() error {
	if c.conn == nil {
		return nil
	}

	if err := c.conn.Drain(); err != nil {
		return fmt.Errorf("drain nats connection: %w", err)
	}

	return nil
}
