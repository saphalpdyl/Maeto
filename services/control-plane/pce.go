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
	previousCosts map[EdgeID]*Cost
}

func NewPCE(costGraph *CostGraph, tenants TenantRepository, reportChan chan<- PathSet, logger *slog.Logger) *PCE {
	return &PCE{
		costGraph:               costGraph,
		tenants:                 tenants,
		logger:                  logger,
		reportChan:              reportChan,
		PathStore:               NewPathStore(),
		Changes:                 NewPathChangeStore(),
		bandwidthReservationReg: NewBandwidthReservationRegistry(),
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
			paths := make(PathSet)
			tickReport := PCETickReport{
				Timestamp:       time.Now(),
				PreviousPathSet: p.PathStore.Load(),
				Logger:          p.logger.With(slog.Time("timestamp", time.Now())),
				Reroutes:        make([]PCETickReportReroute, 0),
				GatedReroutes:   make([]PCETickReportReroute, 0),
			}

			graph.mu.RLock()

			// Blindly compute paths
			for _, n1 := range graph.nodes {
				for _, n2 := range graph.nodes {
					if n1.ID == n2.ID {
						continue
					}

					for _, dim := range []CostDimension{COSTDIM_LATENCY} {
						path, err := p.ComputePath(graph, n1.ID, n2.ID, MinDimension(dim))
						if err != nil {
							p.logger.ErrorContext(ctx, "failed to compute paths", "from", n1.ID, "to", n2.ID, "dimension", dim)
							continue
						}

						paths.Set(n1.ID, n2.ID, dim, path)
					}
				}
			}

			costSnapshot := p.costGraph.Costs()

			// TODO: Have a semi-determinstic priority-based loop
			for _, tenant := range p.tenants.Tenants() {
				for _, tenantSiteFrom := range tenant.Sites {
				TenantLoopInner:
					for _, tenantSiteTo := range tenant.Sites {
						if tenantSiteFrom.Identity == tenantSiteTo.Identity {
							continue
						}

						fromNodeId := NodeID(tenantSiteFrom.Attach)
						toNodeId := NodeID(tenantSiteTo.Attach)

						// TODO: This does overprovisiong, do per POP pair, but im too lazy rn
						requiredReservation := math.Min(tenantSiteFrom.ReservationMbps, tenantSiteTo.ReservationMbps)
						computedPath, ok := paths[fromNodeId][toNodeId][COSTDIM_LATENCY]
						if !ok {
							var err error
							computedPath, err = p.ComputePath(graph, fromNodeId, toNodeId, MinDimension(COSTDIM_LATENCY))
							if err != nil {
								continue
							}
						}

						// Verify that the currently computed path has the bandwidth to support it
						residualOk := true
						for _, edgeID := range computedPath.Edges {
							edge, ok := graph.edges[edgeID]
							if !ok {
								residualOk = false
								p.logger.ErrorContext(ctx, "PCE assigned an unknown Edge ?", slog.String("edgeID", string(edgeID)))
								break
							}

							hasCapacity := p.bandwidthReservationReg.CheckCapacity(edge, requiredReservation)
							if !hasCapacity {
								residualOk = false
								break
							}
						}

						// The naive path exhausted its capacity
						if !residualOk {
							// Prune graph with bandwidth requirements and test
							prunedGraph, err := p.bandwidthReservationReg.PruneGraph(graph, requiredReservation)
							if err != nil {
								continue
							}
							computedPath, err = p.ComputePath(prunedGraph, fromNodeId, toNodeId, MinDimension(COSTDIM_LATENCY))
							if err != nil {
								p.logger.ErrorContext(ctx, "failed to compute paths; likely exhausted paths", "from", fromNodeId, "to", toNodeId)
								continue
							}
						}

						// Get list of *Edge for registering
						computedPathEdges := make([]*Edge, len(computedPath.Edges))
						for i, edgeID := range computedPath.Edges {
							edge, ok := graph.edges[edgeID]
							if !ok {
								p.logger.ErrorContext(ctx, "PCE assigned an unknown Edge ?", slog.String("edgeID", string(edgeID)))
								continue TenantLoopInner
							}

							computedPathEdges[i] = edge
						}

						previousPath := p.PathStore.Get(fromNodeId, toNodeId, COSTDIM_LATENCY)
						if previousPath == nil {
							p.logger.InfoContext(
								ctx,
								"no previous path found; likely first pass",
								slog.String("fromNode", string(fromNodeId)),
								slog.String("toNode", string(toNodeId)),
								slog.String("dimension", string(COSTDIM_LATENCY)),
							)

							err := p.bandwidthReservationReg.Move(nil, computedPathEdges, BandwidthReservationAction{
								ActionType:     BandwidthAdmit,
								TenantSiteFrom: tenantSiteFrom.Identity,
								TenantSiteTo:   tenantSiteTo.Identity,
								Bandwidth:      requiredReservation,
							})
							if err != nil {
								p.logger.ErrorContext(ctx, "failed to admit bandwidth for first pass",
									"from", fromNodeId, "to", toNodeId, log.Err(err))
								continue
							}

							tickReport.Reroutes = append(tickReport.Reroutes, PCETickReportReroute{
								At:       tickReport.Timestamp,
								Src:      fromNodeId,
								Dst:      toNodeId,
								Dim:      COSTDIM_LATENCY,
								Kind:     PathChangeNew,
								FromPath: Path{},
								ToPath:   *computedPath,
							})

							paths.Set(fromNodeId, toNodeId, COSTDIM_LATENCY, computedPath)
							continue
						}

						// The previous path contains cost of the topology then
						// realign score to what it is currently
						previousPath.Cost = p.rescorePath(previousPath, COSTDIM_LATENCY, graph, costSnapshot)

						if previousPath.Equal(computedPath) {
							paths.Set(fromNodeId, toNodeId, COSTDIM_LATENCY, computedPath)
							continue
						}

						// If path change not worth it.
						if (previousPath.Cost / computedPath.Cost) < 1.1 {
							tickReport.GatedReroutes = append(tickReport.GatedReroutes, PCETickReportReroute{
								At:       tickReport.Timestamp,
								Src:      fromNodeId,
								Dst:      toNodeId,
								Dim:      COSTDIM_LATENCY,
								Kind:     PathChangeGated,
								FromPath: *previousPath,
								ToPath:   *computedPath,
								Causes:   attributeCauses(previousPath, COSTDIM_LATENCY, p.previousCosts, costSnapshot),
							})

							paths.Set(fromNodeId, toNodeId, COSTDIM_LATENCY, previousPath)
							continue
						}

						// freeing an edge that left the graph is a no-op
						previousPathEdges := make([]*Edge, 0, len(previousPath.Edges))
						for _, edgeID := range previousPath.Edges {
							if edge, ok := graph.edges[edgeID]; ok {
								previousPathEdges = append(previousPathEdges, edge)
							}
						}

						err := p.bandwidthReservationReg.Move(previousPathEdges, computedPathEdges, BandwidthReservationAction{
							ActionType:     BandwidthAdmit,
							TenantSiteFrom: tenantSiteFrom.Identity,
							TenantSiteTo:   tenantSiteTo.Identity,
							Bandwidth:      requiredReservation,
						})
						if err != nil {
							p.logger.ErrorContext(ctx, "failed to move bandwidth reservation",
								"from", fromNodeId, "to", toNodeId, log.Err(err))
							continue
						}

						tickReport.Reroutes = append(tickReport.Reroutes, PCETickReportReroute{
							At:       tickReport.Timestamp,
							Src:      fromNodeId,
							Dst:      toNodeId,
							Dim:      COSTDIM_LATENCY,
							Kind:     PathChangeRerouted,
							FromPath: *previousPath,
							ToPath:   *computedPath,
							Causes:   attributeCauses(previousPath, COSTDIM_LATENCY, p.previousCosts, costSnapshot),
						})

						paths.Set(fromNodeId, toNodeId, COSTDIM_LATENCY, computedPath)
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

func (p *PCE) rescorePath(path *Path, dim CostDimension, g *Graph, costs map[EdgeID]*Cost) float64 {
	var totalCost float64
	for _, edgeID := range path.Edges {
		_, exists := g.edges[edgeID]
		if !exists {
			return math.Inf(1)
		}

		cost, exists := costs[edgeID]
		if !exists {
			return math.Inf(1)
		}

		totalCost += cost.Costs[dim]
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

func (s *PathStore) Get(from NodeID, to NodeID, dim CostDimension) *Path {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path, exists := s.paths[from][to][dim]
	if !exists {
		return nil
	}

	return path
}

func (s *PathStore) Load() PathSet {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.paths
}
