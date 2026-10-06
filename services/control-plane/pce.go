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

						pathKey := PathKey{
							Tenant: tenant.ID,
							From:   fromNodeId,
							To:     toNodeId,
							Label:  0,
							Dim:    COSTDIM_LATENCY,
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

						computedPath, ok := paths[pathKey]
						if !ok {
							prunedGraph, err := p.bandwidthReservationReg.PruneGraph(pceGraph, tenant.ID, candidateReservation)
							if err != nil {
								continue
							}
							computedPath, err = p.ComputePath(prunedGraph, fromNodeId, toNodeId, MinDimension(COSTDIM_LATENCY))
							if err != nil {
								continue
							}
						}

						// Verify that the currently computed path has the bandwidth to support it
						residualOk := true
						for _, edgeID := range computedPath.Edges {
							edge, ok := pceGraph.edges[edgeID]
							if !ok {
								residualOk = false
								p.logger.ErrorContext(ctx, "PCE assigned an unknown Edge ?", slog.String("edgeID", edgeID.String()))
								break
							}

							hasCapacity := p.bandwidthReservationReg.CheckCapacity(edge, tenant.ID, candidateReservation)
							if !hasCapacity {
								residualOk = false
								break
							}
						}

						// The naive path exhausted its capacity
						if !residualOk {
							// Prune graph with bandwidth requirements and test
							prunedGraph, err := p.bandwidthReservationReg.PruneGraph(pceGraph, tenant.ID, candidateReservation)
							if err != nil {
								continue
							}
							computedPath, err = p.ComputePath(prunedGraph, fromNodeId, toNodeId, MinDimension(COSTDIM_LATENCY))
							if err != nil {
								p.logger.ErrorContext(ctx, "failed to compute paths; likely exhausted paths", "from", fromNodeId, "to", toNodeId)
								continue
							}
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
								Dim:      COSTDIM_LATENCY,
								Kind:     PathChangeNew,
								FromPath: Path{},
								ToPath:   *computedPath,
							})

							paths[pathKey] = computedPath
							continue
						}

						// The previous path contains cost of the topology then
						// realign score to what it is currently
						previousPath.Cost = p.rescorePath(previousPath, COSTDIM_LATENCY, pceGraph, costSnapshot)

						if previousPath.Equal(computedPath) {
							paths[pathKey] = computedPath
							continue
						}

						// If path change not worth it.
						if (previousPath.Cost / computedPath.Cost) < 1.1 {
							tickReport.GatedReroutes = append(tickReport.GatedReroutes, PCETickReportReroute{
								At:       tickReport.Timestamp,
								Tenant:   tenant.ID,
								Label:    pathKey.Label,
								Src:      fromNodeId,
								Dst:      toNodeId,
								Dim:      COSTDIM_LATENCY,
								Kind:     PathChangeGated,
								FromPath: *previousPath,
								ToPath:   *computedPath,
								Causes:   attributeCauses(previousPath, COSTDIM_LATENCY, p.previousCosts, costSnapshot),
							})

							paths[pathKey] = previousPath
							continue
						}

						// freeing an edge that left the graph is a no-op
						err := p.bandwidthReservationReg.Move(ctx, previousPath.Edges, computedPathEdges, tenant.ID, candidateReservation)
						if err != nil {
							p.logger.ErrorContext(ctx, "failed to move bandwidth reservation",
								"from", fromNodeId, "to", toNodeId, log.Err(err))
							continue
						}

						tickReport.Reroutes = append(tickReport.Reroutes, PCETickReportReroute{
							At:       tickReport.Timestamp,
							Tenant:   tenant.ID,
							Label:    pathKey.Label,
							Src:      fromNodeId,
							Dst:      toNodeId,
							Dim:      COSTDIM_LATENCY,
							Kind:     PathChangeRerouted,
							FromPath: *previousPath,
							ToPath:   *computedPath,
							Causes:   attributeCauses(previousPath, COSTDIM_LATENCY, p.previousCosts, costSnapshot),
						})

						paths[pathKey] = computedPath
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

func (p *PCE) rescorePath(path *Path, dim CostDimension, g *PCEGraph, costs map[CostGraphEdge]*Cost) float64 {
	var totalCost float64
	for _, edgeID := range path.Edges {
		edge, exists := g.edges[edgeID]
		if !exists {
			return math.Inf(1)
		}

		cost := edge.Cost(costs)
		if cost == nil {
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
