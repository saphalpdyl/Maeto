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
	costGraph  *CostGraph
	reportChan chan<- PathSet
	logger     *slog.Logger
	PathStore  *PathStore
}

func NewPCE(costGraph *CostGraph, reportChan chan<- PathSet, logger *slog.Logger) *PCE {
	return &PCE{
		costGraph:  costGraph,
		logger:     logger,
		reportChan: reportChan,
		PathStore:  NewPathStore(),
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

			costSnapshot := p.costGraph.Costs()
			for _, srcNode := range graph.nodes {
				for _, destNode := range graph.nodes {
					if srcNode.ID == destNode.ID {
						continue
					}

					path, err := p.ComputePath(graph, srcNode.ID, destNode.ID, func(e *Edge, c *Cost) float64 {
						return c.Costs[COSTDIM_LATENCY]
					})

					if err != nil {
						p.logger.ErrorContext(
							ctx,
							"failed to compute paths",
							log.Err(err),
							slog.String("fromNode", string(srcNode.ID)),
							slog.String("toNode", string(destNode.ID)),
						)
						continue
					}

					previousPath := p.PathStore.Get(srcNode.ID, destNode.ID, COSTDIM_LATENCY)
					// First pass, no previous path to compare to
					if previousPath == nil {
						p.logger.InfoContext(
							ctx,
							"no previous path found; likely first pass",
							slog.String("fromNode", string(srcNode.ID)),
							slog.String("toNode", string(destNode.ID)),
							slog.String("dimension", string(COSTDIM_LATENCY)),
						)

						// Reroute
						tickReport.Reroutes = append(tickReport.Reroutes, PCETickReportReroute{
							Src:      srcNode.ID,
							Dst:      destNode.ID,
							FromPath: Path{},
							ToPath:   *path,
							New:      true,
						})
						paths.Set(srcNode.ID, destNode.ID, COSTDIM_LATENCY, path)

						continue
					}

					previousPath.Cost = p.rescorePath(previousPath, COSTDIM_LATENCY, graph, costSnapshot)

					// Same path just cost changed?
					if previousPath.Equal(path) {
						paths.Set(srcNode.ID, destNode.ID, COSTDIM_LATENCY, path)
						continue
					}

					// Gate reroute
					if (previousPath.Cost / path.Cost) < 1.1 { // Must be at least 10% better
						// Remain at the previous Path
						tickReport.GatedReroutes = append(tickReport.GatedReroutes, PCETickReportReroute{
							Src:      srcNode.ID,
							Dst:      destNode.ID,
							FromPath: *previousPath,
							ToPath:   *path,
							New:      false,
						})

						paths.Set(srcNode.ID, destNode.ID, COSTDIM_LATENCY, previousPath)
						continue
					}

					tickReport.Reroutes = append(tickReport.Reroutes, PCETickReportReroute{
						Src:      srcNode.ID,
						Dst:      destNode.ID,
						FromPath: *previousPath,
						ToPath:   *path,
						New:      false,
					})
					paths.Set(srcNode.ID, destNode.ID, COSTDIM_LATENCY, path)

				}
			}

			p.logger.InfoContext(ctx, "computed all paths", slog.Any("paths", paths))
			graph.mu.RUnlock()

			tickReport.NewPathSet = paths
			tickReport.Log(ctx)

			p.PathStore.Store(paths)
			p.reportChan <- paths

		}
	}
}

func (p *PCE) rescorePath(path *Path, dim CostDimension, g *Graph, costs map[EdgeID]*Cost) float64 {
	var totalCost float64
	for _, edgeID := range path.Edges {
		_, exists := g.edges[edgeID]
		if !exists {
			path.Cost = math.Inf(1)
			break
		}

		totalCost += costs[edgeID].Costs[dim]
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

// Carries information on reroute decisions and metrics
type PCETickReport struct {
	Reroutes []PCETickReportReroute `json:"reroutes"`
	// Reroutes that were gated due to not meeting the reroute threshold
	GatedReroutes []PCETickReportReroute `json:"gated_reroutes"`

	PreviousPathSet PathSet `json:"previous_path_set"`
	NewPathSet      PathSet `json:"new_path_set"`

	Timestamp time.Time `json:"timestamp"`

	Logger *slog.Logger
}

type PCETickReportReroute struct {
	Src NodeID `json:"src"`
	Dst NodeID `json:"dst"`
	New bool   `json:"new"`

	FromPath Path `json:"from_path"`
	ToPath   Path `json:"to_path"`
}

func (r *PCETickReport) Log(ctx context.Context) {
	reroutes := make([]slog.Attr, len(r.Reroutes))
	for i, rr := range r.Reroutes {
		reroutes[i] = slog.Attr{
			Key: fmt.Sprintf("%s>%s", rr.Src, rr.Dst),
			Value: slog.GroupValue(
				slog.Any("from", &rr.FromPath),
				slog.Any("to", &rr.ToPath),
				slog.Bool("new", rr.New),
			),
		}
	}

	gated := make([]slog.Attr, len(r.GatedReroutes))
	for i, rr := range r.GatedReroutes {
		gated[i] = slog.Attr{
			Key: fmt.Sprintf("%s>%s", rr.Src, rr.Dst),
			Value: slog.GroupValue(
				slog.Any("from", &rr.FromPath),
				slog.Any("to", &rr.ToPath),
				slog.Bool("new", rr.New),
			),
		}
	}

	r.Logger.LogAttrs(ctx, slog.LevelInfo, "pce tick report",
		slog.Time("timestamp", r.Timestamp),
		slog.Attr{Key: "reroutes", Value: slog.GroupValue(reroutes...)},
		slog.Attr{Key: "gated_reroutes", Value: slog.GroupValue(gated...)},
	)
}
