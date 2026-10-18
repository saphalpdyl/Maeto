package controlplane

import (
	"cmp"
	"slices"
)

type Demand struct {
	Tenant   *Tenant
	Class    *TrafficClass
	From     NodeID
	To       NodeID
	FromMbps float64
	ToMbps   float64
}

func (d Demand) PathKey() PathKey {
	return PathKey{
		Tenant: d.Tenant.ID,
		From:   d.From,
		To:     d.To,
		Label:  d.Class.Label,
	}
}

func (d Demand) BandwidthReservation() *BandwidthReservation {
	return &BandwidthReservation{
		Key: BandwidthReservationKey{
			SourcePop: d.From,
			SinkPop:   d.To,
			Label:     d.Class.Label,
		},
		SourceBandwidth: d.FromMbps,
		SinkBandwidth:   d.ToMbps,
	}
}

func BuildDemands(tenantRepo TenantRepository) []Demand {
	var demands []Demand

	for _, tenant := range tenantRepo.TenantsByPriority() {
		popsWithLabels := make(map[NodeID]map[uint64]float64)

		// Aggregate (label,mbps) per tenant per site
		//	For a pop that has 2 sites of the same tenant
		// 	we will sum the reservation per label
		// 	For Example:
		// 		PopA-tenant-273[voice] = voice(site1) + voice(site2) // mbps
		// 		PopA-tenant-273[streaming] = streaming(site1) + streaming(site2) // mbps
		for _, site := range tenant.Sites {
			popID := NodeID(site.Attach)
			if popsWithLabels[popID] == nil {
				popsWithLabels[popID] = make(map[uint64]float64)
			}

			// label represents the class e.g. voice, streaming, games etc.
			for label, mbps := range site.ClassMbps {
				popsWithLabels[popID][label] += mbps
			}
		}

		for fromNode, fromClasses := range popsWithLabels {
			for toNode, toClasses := range popsWithLabels {
				if fromNode == toNode {
					continue
				}

				for label, fromMbps := range fromClasses {
					toMbps := toClasses[label]

					if fromMbps <= 0 || toMbps <= 0 {
						continue
					}

					class, exists := tenantRepo.Class(label)
					if !exists {
						continue
					}

					demands = append(demands, Demand{
						Tenant:   tenant,
						Class:    class,
						From:     fromNode,
						To:       toNode,
						FromMbps: fromMbps,
						ToMbps:   toMbps,
					})
				}
			}
		}
	}

	// bulk is the first to find no capacity left. It is still a reserved share of
	// the contract, so an unplaced bulk demand is an unmet contract, not best-effort loss.
	slices.SortFunc(demands, func(a, b Demand) int {
		return cmp.Or(
			// Named classes before default/bulk
			cmp.Compare(isLabelDefault(a.Class.Label), isLabelDefault(b.Class.Label)),

			// Better tier first (higher TierIndex); unranked tiers sort last
			cmp.Compare(b.Tenant.TierIndex, a.Tenant.TierIndex),

			// More important class first (higher Class.Index); default/bulk is 0
			cmp.Compare(b.Class.Index, a.Class.Index),

			// Tie-breakers for a deterministic order only
			cmp.Compare(a.Tenant.ID, b.Tenant.ID),
			cmp.Compare(a.From, b.From),
			cmp.Compare(a.To, b.To),
		)
	})

	return demands
}

func isLabelDefault(l uint64) int {
	if l == DefaultClassLabel {
		return 1
	}

	return 0
}
