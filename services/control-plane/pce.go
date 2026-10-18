package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

const linkStaleAfter = 15 * time.Second

type PCE struct {
	costGraph               *CostGraph
	tenants                 TenantRepository
	bandwidthReservationReg BandwidthReservationRegistry

	reportChan chan<- PathSet
	logger     *slog.Logger
	PathStore  *PathStore
	Changes    *PathChangeStore

	// last tick's costs, kept so a reroute can be attributed to the edges whose
	// cost actually moved rather than just reported as having happened
	previousCosts map[CostGraphEdge]*Cost
}

func NewPCE(costGraph *CostGraph, tenants TenantRepository, reportChan chan<- PathSet, logger *slog.Logger) *PCE {
	return &PCE{
		costGraph:               costGraph,
		tenants:                 tenants,
		logger:                  logger,
		reportChan:              reportChan,
		PathStore:               NewPathStore(),
		Changes:                 NewPathChangeStore(),
		bandwidthReservationReg: NewBandwidthReservationRegistry(logger),
	}
}

func (p *PCE) Run(ctx context.Context, graph *Graph) {
	ticker := time.NewTicker(10 * time.Second)

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Stopping PCE ticker")
			ticker.Stop()
			return
		case <-ticker.C:
			previous := p.PathStore.ClonePaths()
			paths := p.PathStore.ClonePaths()

			tickReport := PCETickReport{
				Timestamp:       time.Now(),
				PreviousPathSet: previous,
				Logger:          p.logger.With(slog.Time("timestamp", time.Now())),
				Reroutes:        make([]PCETickReportReroute, 0),
				GatedReroutes:   make([]PCETickReportReroute, 0),
			}

			p.updateLinkStates(ctx, graph)

			graph.mu.RLock()
			pceGraph := NewPCEGraph(graph)

			costSnapshot := p.costGraph.Costs()

			// TODO: Have a semi-deterministic priority-based loop
			for _, tenant := range p.tenants.Tenants() {
				tenantPops := make(map[NodeID]float64)

				for _, site := range tenant.Sites {
					tenantPops[NodeID(site.Attach)] += site.ReservationMbps
				}

				for nearNode, nearReservation := range tenantPops {
				TenantLoopInner:
					for farNode, farReservation := range tenantPops {
						if nearNode == farNode {
							continue
						}

						fromNodeId := nearNode
						toNodeId := farNode

						for _, dim := range []CostDimension{COSTDIM_LATENCY, COSTDIM_JITTER, COSTDIM_LOSS, CostDimension("default")} {
							pathLabel := DimensionToLabel(dim)

							// Label = Dimension. PathKey is the boundary. Label is a domain-agnostic identifier.
							// 	It can be anything although its being used here to define objectives (latency, jitter etc.)
							// 	Think of it as the 'Color' RFC 9256
							pathKey := PathKey{
								Tenant: tenant.ID,
								From:   fromNodeId,
								To:     toNodeId,
								Label:  pathLabel,
							}

							candidateReservation := &BandwidthReservation{
								Key: BandwidthReservationKey{
									SourcePop: nearNode,
									SinkPop:   farNode,
									Label:     pathKey.Label,
								},
								SourceBandwidth: nearReservation,
								SinkBandwidth:   farReservation,
							}

							// Prune Graph with existing bandwidth reservations
							prunedGraph, err := p.bandwidthReservationReg.PruneGraph(pceGraph, tenant.ID, candidateReservation)
							if err != nil {
								continue
							}

							var objectiveFunc Objective
							if pathKey.Label == 0 {
								// Bulk/Default class
								objectiveFunc = StaticBandwidthUtilization(p.bandwidthReservationReg.ReservedMapExcluding(tenant.ID, candidateReservation.Key))
							} else {
								objectiveFunc = MinDimension(dim)
							}

							computedPath, err := p.ComputePath(prunedGraph, fromNodeId, toNodeId, objectiveFunc)
							if err != nil {
								p.logger.WarnContext(ctx, "no path fits the reservation; keeping previous", "from", fromNodeId, "to", toNodeId, log.Err(err))
								continue
							}

							// Get list of *PCEEdge for registering
							computedPathEdges := make([]*PCEEdge, len(computedPath.Edges))
							for i, edgeID := range computedPath.Edges {
								edge, ok := pceGraph.edges[edgeID]
								if !ok {
									p.logger.ErrorContext(ctx, "PCE assigned an unknown Edge ?", slog.String("edgeID", edgeID.String()))
									continue TenantLoopInner
								}

								computedPathEdges[i] = edge
							}

							previousPath := previous[pathKey]
							if previousPath == nil {
								p.logger.InfoContext(
									ctx,
									"no previous path found; likely first pass",
									slog.String("fromNode", string(fromNodeId)),
									slog.String("toNode", string(toNodeId)),
									slog.String("dimension", string(COSTDIM_LATENCY)),
								)

								err := p.bandwidthReservationReg.Move(ctx, nil, computedPathEdges, tenant.ID, candidateReservation)
								if err != nil {
									p.logger.ErrorContext(ctx, "failed to admit bandwidth for first pass",
										"from", fromNodeId, "to", toNodeId, log.Err(err))
									continue
								}

								tickReport.Reroutes = append(tickReport.Reroutes, PCETickReportReroute{
									At:       tickReport.Timestamp,
									Tenant:   tenant.ID,
									Label:    pathKey.Label,
									Src:      fromNodeId,
									Dst:      toNodeId,
									Kind:     PathChangeNew,
									FromPath: Path{},
									ToPath:   *computedPath,
								})

								paths[pathKey] = computedPath
								continue
							}

							// The previous path contains cost of the topology then
							// realign score to what it is currently
							previousCost := p.rescorePath(previousPath, objectiveFunc, pceGraph, costSnapshot)
							previousPathIsBroken := math.IsInf(previousCost, 1)
							if !previousPathIsBroken {
								previousPath.Cost = previousCost
							}

							if previousPath.Equal(computedPath) {
								paths[pathKey] = computedPath
								continue
							}

							// If path change not worth it.
							if !previousPathIsBroken && (previousPath.Cost/computedPath.Cost) < 1.1 {
								tickReport.GatedReroutes = append(tickReport.GatedReroutes, PCETickReportReroute{
									At:       tickReport.Timestamp,
									Tenant:   tenant.ID,
									Label:    pathKey.Label,
									Src:      fromNodeId,
									Dst:      toNodeId,
									Kind:     PathChangeGated,
									FromPath: *previousPath,
									ToPath:   *computedPath,
									Causes:   attributeCauses(previousPath, COSTDIM_LATENCY, p.previousCosts, costSnapshot),
								})

								paths[pathKey] = previousPath
								continue
							}

							// freeing an edge that left the graph is a no-op
							err = p.bandwidthReservationReg.Move(ctx, previousPath.Edges, computedPathEdges, tenant.ID, candidateReservation)
							if err != nil {
								p.logger.ErrorContext(ctx, "failed to move bandwidth reservation",
									"from", fromNodeId, "to", toNodeId, log.Err(err))
								continue
							}

							rerouteKind := PathChangeRerouted
							if previousPathIsBroken {
								rerouteKind = PathChangeLinkDown
							}

							tickReport.Reroutes = append(tickReport.Reroutes, PCETickReportReroute{
								At:       tickReport.Timestamp,
								Tenant:   tenant.ID,
								Label:    pathKey.Label,
								Src:      fromNodeId,
								Dst:      toNodeId,
								Kind:     rerouteKind,
								FromPath: *previousPath,
								ToPath:   *computedPath,
								Causes:   attributeCauses(previousPath, COSTDIM_LATENCY, p.previousCosts, costSnapshot),
							})

							paths[pathKey] = computedPath
						}
					}
				}
			}

			// the channel send can block; nothing below reads the graph
			graph.mu.RUnlock()

			tickReport.NewPathSet = paths
			tickReport.Log(ctx)

			p.PathStore.Store(paths)
			p.Changes.Record(tickReport.Reroutes...)
			p.Changes.Record(tickReport.GatedReroutes...)
			p.previousCosts = costSnapshot

			p.reportChan <- paths
		}
	}
}

// A link that has been probed before but has gone quiet is treated as down.
// Links that were never probed keep whatever state the topology gave them.
func (p *PCE) updateLinkStates(ctx context.Context, graph *Graph) {
	lastSeen := p.costGraph.LastSeen()
	now := time.Now()

	graph.mu.Lock()
	defer graph.mu.Unlock()

	for _, edge := range graph.edges {
		seenAt, probed := lastSeen[edge.ID]
		if !probed {
			continue
		}

		silentFor := now.Sub(seenAt)
		up := silentFor < linkStaleAfter

		if up == edge.Up {
			continue
		}

		edge.Up = up

		if up {
			p.logger.InfoContext(ctx, "link is back up", slog.String("edge", string(edge.ID)))
		} else {
			p.logger.WarnContext(ctx, "link is down; no probe results",
				slog.String("edge", string(edge.ID)),
				slog.Duration("silent_for", silentFor))
		}
	}
}

// rescorePath rescores the previous path with current cost snapshot
func (p *PCE) rescorePath(path *Path, objective Objective, g *PCEGraph, costs map[CostGraphEdge]*Cost) float64 {
	var totalCost float64
	for _, edgeID := range path.Edges {
		edge, exists := g.edges[edgeID]
		if !exists {
			return math.Inf(1)
		}

		totalCost += objective(edge, edge.Cost(costs))
	}

	return totalCost
}

type PathStore struct {
	mu    sync.RWMutex
	paths PathSet
}

func NewPathStore() *PathStore {
	return &PathStore{paths: make(PathSet)}
}

func (s *PathStore) Store(paths PathSet) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.paths = paths
}

func (s *PathStore) Get(key PathKey) *Path {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.paths[key]
}

func (s *PathStore) Load() PathSet {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.paths
}

func (s *PathStore) ClonePaths() PathSet {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	newPaths := make(PathSet)

	for key, path := range s.paths {
		cp := *path
		newPaths[key] = &cp
	}

	return newPaths
}

// DimensionToLabel Build this as a stackable switch
// Since label is domain-agnostic it doesn't always have to mean
//
//	dimension. It can mean other classification. Hence, the default
//	value should be 0 so that other xxxToLabel function can iterat
func DimensionToLabel(dim CostDimension) uint64 {
	switch dim {
	case COSTDIM_LATENCY:
		return 1
	case COSTDIM_JITTER:
		return 2
	case COSTDIM_LOSS:
		return 3
	default:
		return 0
	}
}
