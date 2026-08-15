package maetoportal

import (
	"context"
	"errors"
	"log/slog"

	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
)

type Portal struct {
	InstanceId string

	config  PortalConfig
	logger  *slog.Logger
	control ControlClient
	tunnel  Tunnel
}

func NewPortal(
	instanceId string,
	config PortalConfig,
	control ControlClient,
	tunnel Tunnel,
	logger *slog.Logger,
) *Portal {
	return &Portal{
		InstanceId: instanceId,
		config:     config,
		control:    control,
		tunnel:     tunnel,
		logger:     logger,
	}
}

func (p *Portal) Run(ctx context.Context) error {
	p.logger.InfoContext(ctx, "portal starting",
		log.PortalID(p.config.PortalID),
		log.PopID(p.config.AttachPop),
	)

	if err := p.control.Connect(ctx); err != nil {
		return err
	}
	defer func() {
		if err := p.control.Close(); err != nil {
			p.logger.ErrorContext(ctx, "failed to close control client", log.Err(err))
		}
	}()

	if p.tunnel != nil {
		if err := p.tunnel.Up(ctx); err != nil {
			return err
		}
	}

	p.logger.InfoContext(ctx, "portal ready", log.PortalID(p.config.PortalID))

	<-ctx.Done()

	return p.shutdown(ctx)
}

func (p *Portal) shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.ShutdownGracePeriod)
	defer cancel()

	p.logger.InfoContext(ctx, "portal shutting down",
		log.DurationMs(p.config.ShutdownGracePeriod.Milliseconds()),
	)

	var errs []error
	if p.tunnel != nil {
		if err := p.tunnel.Down(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
