package maetoagent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type Reconciler struct {
	dp Dataplane

	intentFeed <-chan *NodeIntent

	logger *slog.Logger
}

func NewReconciler(dp Dataplane, logger *slog.Logger, intentFeed <-chan *NodeIntent) *Reconciler {
	return &Reconciler{
		dp:         dp,
		intentFeed: intentFeed,
		logger:     logger,
	}
}

func (r *Reconciler) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case intent := <-r.intentFeed:
			r.logger.InfoContext(ctx, "reconciler received intent", "intent", intent)
			if err := r.Reconcile(ctx, intent); err != nil {
				r.logger.ErrorContext(ctx, "reconcile failed", log.Err(err))
			}
		}
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, intent *NodeIntent) error {
	for _, cust := range intent.CustomerBasedIntents {
		if cust.Gen > 1 {
			r.logger.ErrorContext(ctx, "reconciliation against existing states is not yet supported", slog.Any("intent", intent))
			continue
		}
		for _, site := range cust.Sites {
			vrfTableName := fmt.Sprintf("vrf-cust-%d", site.CustomerID)

			if err := r.dp.AddVRF(ctx, vrfTableName, site.CustomerID+1000); err != nil {
				r.logger.ErrorContext(ctx, "failed to add VRF", log.Err(err))
				continue
			}
			r.logger.InfoContext(ctx, "added VRF", slog.String("vrfTableName", vrfTableName), slog.Int("customerID", site.CustomerID))

			tunnelIface := fmt.Sprintf("xfrm-%d-%d", site.CustomerID, site.IfID)
			if err := r.dp.InsertXFRMInterface(ctx, tunnelIface, "eth1", site.IfID, vrfTableName); err != nil {
				r.logger.ErrorContext(ctx, "failed to insert xfrm interface", log.Err(err))
				continue
			}
			r.logger.InfoContext(ctx, "inserted XFRM interface", slog.String("interfaceName", tunnelIface), slog.String("vrfTableName", vrfTableName))

			if err := r.dp.InsertReturnPrefix(ctx, tunnelIface, vrfTableName, site.Prefix.String()); err != nil {
				r.logger.ErrorContext(ctx, "failed to insert return prefix", log.Err(err))
				continue
			}
			r.logger.InfoContext(ctx, "inserted return prefix", slog.String("prefix", site.Prefix.String()), slog.String("vrfTableName", vrfTableName))
		}
	}

	return nil
}
