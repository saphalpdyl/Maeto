package dataplane

import (
	"context"
	"log/slog"

	"github.com/saphalpdyl/maeto/libs/dataplane/log"
)

func (r *Reconciler) Apply(ctx context.Context, diff DiffResult) error {
	err := r.dp.EnsureClassificationBase()
	if err != nil {
		return err
	}

	// First of all, we remove shit
	// We remove from bottom up i.e route -> xfrm -> vrf
	// TODO: Later group by kind in a map so that we don't traverse everything everytime
	for _, kind := range []Kind{KindClassification, KindRule, KindSRRoute, KindSID, KindRoute, KindXFRM, KindVRF} {
		for _, resource := range diff.Remove {
			if resource.ID().Kind != kind {
				continue
			}

			switch res := resource.(type) {
			case *Route:
				if err := r.dp.RemovePrefixRoute(res.Dst, res.Table); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove route", log.Err(err))
					break
				}

			case *XFRM:
				if err := r.dp.RemoveXFRMInterface(res.Index); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove xfrm interface", log.Err(err))
					break
				}

			case *VRF:
				if err := r.dp.RemoveVRF(res.Index); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove vrf", log.Err(err))
					break
				}

			case *Rule:
				if err := r.dp.RemoveRule(res.Priority, res.Src, res.Table, res.Mark, res.Iif); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove rule", log.Err(err))
					break
				}
			case *SID:
				if err := r.dp.RemoveSID(res.SID); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove sid", log.Err(err))
					break
				}
			case *SRRoute:
				if err := r.dp.RemoveSRRouteForPrefix(res.Prefix, res.Table); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove SR Route", log.Err(err))
					break
				}
			case *Classification:
				if err := r.dp.RemoveClassification(res.Classifier); err != nil {
					r.logger.ErrorContext(ctx, "failed to remove classifier", log.Err(err))
					break
				}
			}

		}
	}

	// Addition
	for _, kind := range []Kind{KindVRF, KindXFRM, KindRoute, KindSID, KindSRRoute, KindRule, KindClassification} {
		for _, resource := range diff.Add {
			if resource.ID().Kind != kind {
				continue
			}

			switch res := resource.(type) {
			case *VRF:
				if err := r.dp.UpsertVRF(res.Name, int(res.TableID)); err != nil {
					r.logger.ErrorContext(ctx, "failed to upsert VRF", log.Err(err))
				}

			case *XFRM:
				mVRF := (*string)(nil)
				if res.MasterVRF != "" {
					mVRF = &res.MasterVRF // During rendering, masterVRF is set to "" for CPEs
				}

				parent := res.Parent
				if parent == "" {
					parent = "lo"
				}

				if err := r.dp.InsertXFRMInterface(res.Name, parent, res.IfID, mVRF); err != nil {
					r.logger.ErrorContext(ctx, "failed to insert XFRM", log.Err(err))
				}

			case *Route:
				if err := r.dp.InsertPrefixRoute(res.Dev, res.Table, res.Dst, res.Via); err != nil {
					r.logger.ErrorContext(ctx, "failed to insert Route", log.Err(err))
				}

			case *SRRoute:
				if err := r.dp.InsertSRRouteForPrefix(res.Prefix, res.Table, res.Segments); err != nil {
					r.logger.ErrorContext(ctx, "failed to insert SR Route", log.Err(err))
				}

			case *Rule:
				if err := r.dp.UpsertRule(res.Priority, res.Src, res.Table, res.Mark, res.Iif); err != nil {
					r.logger.ErrorContext(ctx, "failed to upsert rule", log.Err(err))
				}

			case *SID:
				if res.SIDType != SIDDT46 {
					r.logger.WarnContext(ctx, "SID Type is not supported", slog.String("sid_type", string(res.SIDType)))
				}

				if err := r.dp.UpsertDT46SID(res.SID, res.TableID); err != nil {
					r.logger.ErrorContext(ctx, "failed to upsert DT46 SID", log.Err(err))
				}
			case *Classification:
				if err := r.dp.InsertClassification(res.Classifier, res.Mark); err != nil {
					r.logger.ErrorContext(ctx, "failed to insert Classification", log.Err(err))
				}
			}

		}
	}

	return nil
}
