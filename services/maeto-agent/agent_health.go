package maetoagent

import (
	"context"
	"log/slog"
	"maps"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type HealthMonitor struct {
	bfd    *BFDListener
	logger *slog.Logger

	updates chan<- map[string]nodesync.LinkState

	bfdReportChan chan BFDMessage

	linkStates map[string]nodesync.LinkState
}

func NewHealthMonitor(updates chan<- map[string]nodesync.LinkState, logger *slog.Logger) *HealthMonitor {
	bfdReportChan := make(chan BFDMessage)

	bfd := NewBFD(DefaultMaetoSocketPath, bfdReportChan, logger.With(log.Domain(log.DomainBFDListener)))

	return &HealthMonitor{
		bfd:           bfd,
		logger:        logger,
		updates:       updates,
		bfdReportChan: bfdReportChan,
		linkStates:    make(map[string]nodesync.LinkState),
	}
}

func (h *HealthMonitor) Start(ctx context.Context) error {
	go func() {
		err := h.bfd.Start(ctx)
		if err != nil && ctx.Err() == nil {
			h.logger.ErrorContext(ctx, "failed to start bfd listener", log.Err(err))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <-h.bfdReportChan:
			if m.Type == BFDMessageSnapshot || m.Type == BFDMessageDisconnected {
				for link := range h.linkStates {
					h.linkStates[link] = nodesync.LinkStateUnknown
				}
			}

			for _, sess := range m.Sessions {
				if sess.Key.Interface == "" {
					continue
				}

				state := nodesync.LinkStateUnknown
				if sess.State == BFDStateAdminDown || sess.State == BFDStateDown {
					state = nodesync.LinkStateDown
				} else if sess.State == BFDStateInit {
					// skip
					continue
				} else if sess.State == BFDStateUp {
					state = nodesync.LinkStateUp
				}

				h.linkStates[sess.Key.Interface] = state
			}

			select {
			case h.updates <- maps.Clone(h.linkStates):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}
