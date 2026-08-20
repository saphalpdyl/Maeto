package maetoagent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type Reconciler struct {
	dp dataplane.Dataplane

	intentFeed <-chan *NodeIntent

	logger *slog.Logger
}

func NewReconciler(dp dataplane.Dataplane, logger *slog.Logger, intentFeed <-chan *NodeIntent) *Reconciler {
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
	for _, tenant := range intent.TenantIntents {
		for _, site := range tenant.Sites {
			vrfTableName := fmt.Sprintf("vrf-tenant-%d", site.TenantID)

			if err := r.dp.AddVRF(ctx, vrfTableName, site.TenantID+1000); err != nil {
				r.logger.ErrorContext(ctx, "failed to add VRF", log.Err(err))
				continue
			}
			r.logger.InfoContext(ctx, "added VRF", slog.String("vrfTableName", vrfTableName), slog.Int("tenantID", site.TenantID))

			tunnelIface := fmt.Sprintf("xfrm-%d-%d", site.TenantID, site.IfID)
			if err := r.dp.InsertXFRMInterface(ctx, tunnelIface, "eth1", site.IfID, vrfTableName); err != nil {
				r.logger.ErrorContext(ctx, "failed to insert xfrm interface", log.Err(err))
				continue
			}
			r.logger.InfoContext(ctx, "inserted XFRM interface", slog.String("interfaceName", tunnelIface), slog.String("vrfTableName", vrfTableName))

			if err := r.dp.InsertReturnPrefix(ctx, tunnelIface, vrfTableName, site.Prefix); err != nil {
				r.logger.ErrorContext(ctx, "failed to insert return prefix", log.Err(err))
				continue
			}
			r.logger.InfoContext(ctx, "inserted return prefix", slog.String("prefix", site.Prefix.String()), slog.String("vrfTableName", vrfTableName))
		}
	}

	return nil
}
