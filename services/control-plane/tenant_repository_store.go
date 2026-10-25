package controlplane

import (
	"context"
	"fmt"

	"github.com/saphalpdyl/maeto/libs/store"
)

func (r *JSONTenantRepository) SaveTo(ctx context.Context, q *store.Queries) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if err := q.DeleteAllTenants(ctx); err != nil {
		return fmt.Errorf("failed to delete tenants: %w", err)
	}

	if err := q.DeleteAllTrafficClasses(ctx); err != nil {
		return fmt.Errorf("failed to delete traffic classes: %w", err)
	}

	for _, class := range r.classes {
		err := q.CreateTrafficClass(ctx, store.CreateTrafficClassParams{
			Label:      int64(class.Label),
			Name:       class.Name,
			ClassIndex: int32(class.Index),
		})
		if err != nil {
			return fmt.Errorf("failed to create traffic class %s: %w", class.Name, err)
		}

		for _, dscp := range class.Match.DSCP {
			err := q.AddTrafficClassDSCP(ctx, store.AddTrafficClassDSCPParams{
				Label: int64(class.Label),
				Dscp:  int16(dscp),
			})
			if err != nil {
				return fmt.Errorf("failed to add dscp %d to class %s: %w", dscp, class.Name, err)
			}
		}

		for dimension, weight := range class.Objective {
			err := q.SetTrafficClassObjective(ctx, store.SetTrafficClassObjectiveParams{
				Label:     int64(class.Label),
				Dimension: string(dimension),
				Weight:    weight,
			})
			if err != nil {
				return fmt.Errorf("failed to set objective %s on class %s: %w", dimension, class.Name, err)
			}
		}
	}

	for _, tenant := range r.byID {
		err := q.CreateTenant(ctx, store.CreateTenantParams{
			ID:         int32(tenant.ID),
			Allocation: tenant.Allocation,
			Tier:       tenant.Tier,
			TierIndex:  int32(tenant.TierIndex),
			VrfTable:   int32(tenant.VRFTable),
		})
		if err != nil {
			return fmt.Errorf("failed to create tenant %d: %w", tenant.ID, err)
		}

		for _, site := range tenant.Sites {
			err := q.CreateSite(ctx, store.CreateSiteParams{
				PortalID:        site.PortalID,
				TenantID:        int32(site.TenantID),
				Cpe:             site.CPE,
				Node:            site.Node,
				Prefix:          site.Prefix,
				Attach:          site.Attach,
				AttachNode:      site.AttachNode,
				IfID:            int64(site.IfID),
				Identity:        site.Identity,
				ReservationMbps: site.ReservationMbps,
			})
			if err != nil {
				return fmt.Errorf("failed to create site %s: %w", site.PortalID, err)
			}

			for label, mbps := range site.ClassMbps {
				err := q.SetSiteClassReservation(ctx, store.SetSiteClassReservationParams{
					PortalID: site.PortalID,
					Label:    int64(label),
					Mbps:     mbps,
				})
				if err != nil {
					return fmt.Errorf("failed to set class %d reservation on site %s: %w", label, site.PortalID, err)
				}
			}
		}
	}

	return nil
}

func (r *JSONTenantRepository) LoadFrom(ctx context.Context, q *store.Queries) error {
	classes, err := loadTrafficClasses(ctx, q)
	if err != nil {
		return err
	}

	tenantRows, err := q.ListTenants(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tenants: %w", err)
	}

	byID := make(map[TenantID]*Tenant, len(tenantRows))
	for _, row := range tenantRows {
		byID[TenantID(row.ID)] = &Tenant{
			ID:         TenantID(row.ID),
			Allocation: row.Allocation,
			Tier:       row.Tier,
			TierIndex:  int(row.TierIndex),
			VRFTable:   int(row.VrfTable),
		}
	}

	siteRows, err := q.ListSites(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sites: %w", err)
	}

	byIdentity := map[string]*Site{}
	byPortalID := map[string]*Site{}
	byPop := map[string][]*Site{}

	for _, row := range siteRows {
		tenant, exists := byID[TenantID(row.TenantID)]
		if !exists {
			return fmt.Errorf("site %s belongs to unknown tenant %d", row.PortalID, row.TenantID)
		}

		reservations, err := q.ListSiteClassReservations(ctx, row.PortalID)
		if err != nil {
			return fmt.Errorf("failed to list class reservations for site %s: %w", row.PortalID, err)
		}

		classMbps := make(map[uint64]float64, len(reservations))
		for _, reservation := range reservations {
			classMbps[uint64(reservation.Label)] = reservation.Mbps
		}

		site := &Site{
			TenantID:        TenantID(row.TenantID),
			CPE:             row.Cpe,
			PortalID:        row.PortalID,
			Node:            row.Node,
			Prefix:          row.Prefix,
			Attach:          row.Attach,
			AttachNode:      row.AttachNode,
			IfID:            uint32(row.IfID),
			Identity:        row.Identity,
			ReservationMbps: row.ReservationMbps,
			ClassMbps:       classMbps,
		}

		byIdentity[site.Identity] = site
		byPortalID[site.PortalID] = site
		byPop[site.Attach] = append(byPop[site.Attach], site)
		tenant.Sites = append(tenant.Sites, site)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.classes = classes
	r.byID = byID
	r.byIdentity = byIdentity
	r.byPortalID = byPortalID
	r.byPop = byPop

	return nil
}

func loadTrafficClasses(ctx context.Context, q *store.Queries) (map[uint64]*TrafficClass, error) {
	rows, err := q.ListTrafficClasses(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list traffic classes: %w", err)
	}

	classes := make(map[uint64]*TrafficClass, len(rows))

	for _, row := range rows {
		dscpRows, err := q.ListTrafficClassDSCP(ctx, row.Label)
		if err != nil {
			return nil, fmt.Errorf("failed to list dscp for class %s: %w", row.Name, err)
		}

		var dscp []int
		for _, dscpRow := range dscpRows {
			dscp = append(dscp, int(dscpRow.Dscp))
		}

		objectiveRows, err := q.ListTrafficClassObjectives(ctx, row.Label)
		if err != nil {
			return nil, fmt.Errorf("failed to list objectives for class %s: %w", row.Name, err)
		}

		var objective map[CostDimension]float64
		if len(objectiveRows) > 0 {
			objective = make(map[CostDimension]float64, len(objectiveRows))
		}
		for _, objectiveRow := range objectiveRows {
			objective[CostDimension(objectiveRow.Dimension)] = objectiveRow.Weight
		}

		classes[uint64(row.Label)] = &TrafficClass{
			Name:      row.Name,
			Label:     uint64(row.Label),
			Index:     int(row.ClassIndex),
			Match:     ClassMatch{DSCP: dscp},
			Objective: objective,
		}
	}

	return classes, nil
}
