package failover

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/saphalpdyl/maeto/libs/failover/log"
	"github.com/saphalpdyl/maeto/libs/nodesync"
)

// Consumes Linkstates, compares and triggers
type FailoverLatch struct {
	mu sync.Mutex

	linkStates       map[string]nodesync.LinkState
	linkStateUpdates <-chan map[string]nodesync.LinkState

	grouping map[string][]string // egress (NodeID) -> interface names
	wasDown  map[string]bool

	logger *slog.Logger

	activateRepairFunc func(string) error
	resetRepairFunc    func(string) error

	// without this, the latch activates on CPE start when bfdd is coming up
	// the latch should only activate on up -> down transition
	seenUp map[string]bool

	ready bool
}

func NewFailoverLatch(
	linkStateUpdatesChan <-chan map[string]nodesync.LinkState,
	logger *slog.Logger,
	ActivateRepairFunc func(string) error,
	ResetRepairFunc func(string) error,
) *FailoverLatch {
	return &FailoverLatch{
		linkStates:         make(map[string]nodesync.LinkState),
		linkStateUpdates:   linkStateUpdatesChan,
		logger:             logger,
		activateRepairFunc: ActivateRepairFunc,
		resetRepairFunc:    ResetRepairFunc,
		grouping:           make(map[string][]string),
		wasDown:            make(map[string]bool),
		ready:              false,
		seenUp:             make(map[string]bool),
	}
}

func (f *FailoverLatch) SetGrouping(grouping map[string][]string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for egress, ifaces := range grouping {
		if !slices.Equal(f.grouping[egress], ifaces) {
			delete(f.seenUp, egress)
		}
	}

	f.grouping = grouping
}

func (f *FailoverLatch) MarkReady() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ready = true
}

func (f *FailoverLatch) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case linkStates := <-f.linkStateUpdates:
			f.evaluate(ctx, linkStates)
		}
	}
}

func (f *FailoverLatch) evaluate(ctx context.Context, linkStates map[string]nodesync.LinkState) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.linkStates = linkStates

	for egress, ifaces := range f.grouping {
		if groupUp(ifaces, linkStates) {
			f.seenUp[egress] = true
		}

		down := groupDown(linkStates, ifaces)

		if f.ready && down && !f.wasDown[egress] && f.seenUp[egress] {
			f.logger.InfoContext(ctx, "egress down, activating repair", slog.String("egress", egress))
			if err := f.activateRepairFunc(egress); err != nil {
				f.logger.ErrorContext(ctx, "failed to activate repair", slog.String("egress", egress), log.Err(err))
			}
		}

		f.wasDown[egress] = down
	}
}

func groupUp(ifaces []string, linkStates map[string]nodesync.LinkState) bool {
	for _, iface := range ifaces {
		if linkStates[iface] != nodesync.LinkStateUp {
			return false
		}
	}

	return len(ifaces) > 0
}

func (f *FailoverLatch) Recover(ctx context.Context, downNodes []string) {
	f.mu.Lock()
	egresses := make([]string, 0, len(f.grouping))
	for egress := range f.grouping {
		if !slices.Contains(downNodes, egress) {
			egresses = append(egresses, egress)
		}
	}
	f.mu.Unlock()

	for _, egress := range egresses {
		if err := f.Reset(ctx, egress); err != nil {
			f.logger.ErrorContext(ctx, "failed to reset repair", slog.String("egress", egress), log.Err(err))
		}
	}
}

func (f *FailoverLatch) Reset(ctx context.Context, egress string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if groupDown(f.linkStates, f.grouping[egress]) {
		return nil
	}

	return f.resetRepairFunc(egress)
}

func groupDown(linkStates map[string]nodesync.LinkState, ifaces []string) bool {
	for _, iface := range ifaces {
		if linkStates[iface] != nodesync.LinkStateDown {
			return false
		}
	}

	return len(ifaces) > 0
}
