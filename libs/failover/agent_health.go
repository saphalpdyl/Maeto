package failover

import (
	"context"
	"log/slog"
	"maps"
	"sync"

	"github.com/saphalpdyl/maeto/libs/failover/log"
	"github.com/saphalpdyl/maeto/libs/nodesync"
)

type HealthMonitor struct {
	mu     sync.Mutex
	bfd    *BFDListener
	logger *slog.Logger

	updates []chan map[string]nodesync.LinkState

	bfdReportChan chan BFDMessage

	linkStates map[string]nodesync.LinkState
}

func NewHealthMonitor(logger *slog.Logger) *HealthMonitor {
	bfdReportChan := make(chan BFDMessage)

	bfd := NewBFD(DefaultMaetoSocketPath, bfdReportChan, logger)

	return &HealthMonitor{
		bfd:           bfd,
		logger:        logger,
		updates:       make([]chan map[string]nodesync.LinkState, 0),
		bfdReportChan: bfdReportChan,
		linkStates:    make(map[string]nodesync.LinkState),
	}
}

func (h *HealthMonitor) CreateConsumer() <-chan map[string]nodesync.LinkState {
	h.mu.Lock()
	defer h.mu.Unlock()

	updateChan := make(chan map[string]nodesync.LinkState, 1)
	h.updates = append(h.updates, updateChan)
	return updateChan
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

			for _, update := range h.updates {
				select {
				case update <- maps.Clone(h.linkStates):
				default:
					select {
					case <-update:
					default:
					}

					select {
					case update <- maps.Clone(h.linkStates):
					default:
					}
				}
			}
		}
	}
}
