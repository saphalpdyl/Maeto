package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

type PCE struct {
	costGraph  *CostGraph
	reportChan chan<- PathSet
	logger     *slog.Logger
	PathStore  *PathStore
	Changes    *PathChangeStore

	// last tick's costs, kept so a reroute can be attributed to the edges whose
	// cost actually moved rather than just reported as having happened
	previousCosts map[EdgeID]*Cost
}

func NewPCE(costGraph *CostGraph, reportChan chan<- PathSet, logger *slog.Logger) *PCE {
	return &PCE{
		costGraph:  costGraph,
		logger:     logger,
		reportChan: reportChan,
		PathStore:  NewPathStore(),
		Changes:    NewPathChangeStore(),
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
							At:       tickReport.Timestamp,
							Src:      srcNode.ID,
							Dst:      destNode.ID,
							Dim:      COSTDIM_LATENCY,
							Kind:     PathChangeNew,
							FromPath: Path{},
							ToPath:   *path,
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
							At:       tickReport.Timestamp,
							Src:      srcNode.ID,
							Dst:      destNode.ID,
							Dim:      COSTDIM_LATENCY,
							Kind:     PathChangeGated,
							FromPath: *previousPath,
							ToPath:   *path,
							Causes:   attributeCauses(previousPath, COSTDIM_LATENCY, p.previousCosts, costSnapshot),
						})

						paths.Set(srcNode.ID, destNode.ID, COSTDIM_LATENCY, previousPath)
						continue
					}

					tickReport.Reroutes = append(tickReport.Reroutes, PCETickReportReroute{
						At:       tickReport.Timestamp,
						Src:      srcNode.ID,
						Dst:      destNode.ID,
						Dim:      COSTDIM_LATENCY,
						Kind:     PathChangeRerouted,
						FromPath: *previousPath,
						ToPath:   *path,
						Causes:   attributeCauses(previousPath, COSTDIM_LATENCY, p.previousCosts, costSnapshot),
					})
					paths.Set(srcNode.ID, destNode.ID, COSTDIM_LATENCY, path)

				}
			}

			p.logger.InfoContext(ctx, "computed all paths", slog.Any("paths", paths))
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

type PathChangeKind string

const (
	PathChangeRerouted PathChangeKind = "rerouted"
	PathChangeGated    PathChangeKind = "gated"
	PathChangeNew      PathChangeKind = "new"
)

type PCETickReportReroute struct {
	At   time.Time      `json:"at"`
	Src  NodeID         `json:"src"`
	Dst  NodeID         `json:"dst"`
	Dim  CostDimension  `json:"dimension"`
	Kind PathChangeKind `json:"kind"`

	FromPath Path `json:"from_path"`
	ToPath   Path `json:"to_path"`

	// edges of the departing path whose cost rose, ranked by how much of the
	// increase each accounts for. empty on a first-pass path.
	Causes []PathChangeCause `json:"causes,omitempty"`
}

type PathChangeCause struct {
	Edge  EdgeID        `json:"edge"`
	Dim   CostDimension `json:"dimension"`
	Was   float64       `json:"was"`
	Now   float64       `json:"now"`
	Delta float64       `json:"delta"`
	Share float64       `json:"share"`
}

// an edge has to carry at least this share of the increase to be named, so a
// reroute is not explained by a list of rounding errors
const causeShareFloor = 0.05

// attributeCauses ranks the departing path's edges by how much their cost rose
// between two snapshots. Edges that got cheaper are dropped -- they are not why
// the path was abandoned.
func attributeCauses(from *Path, dim CostDimension, was, now map[EdgeID]*Cost) []PathChangeCause {
	if from == nil || len(was) == 0 {
		return nil
	}

	causes := make([]PathChangeCause, 0, len(from.Edges))
	total := 0.0

	for _, id := range from.Edges {
		prev, hadPrev := was[id]
		curr, hasCurr := now[id]
		if !hadPrev || !hasCurr || prev == nil || curr == nil {
			continue
		}

		delta := curr.Costs[dim] - prev.Costs[dim]
		if delta <= 0 {
			continue
		}

		total += delta
		causes = append(causes, PathChangeCause{
			Edge:  id,
			Dim:   dim,
			Was:   prev.Costs[dim],
			Now:   curr.Costs[dim],
			Delta: delta,
		})
	}

	if total <= 0 {
		return nil
	}

	ranked := causes[:0]
	for _, c := range causes {
		c.Share = c.Delta / total
		if c.Share < causeShareFloor {
			continue
		}

		ranked = append(ranked, c)
	}

	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Delta > ranked[j].Delta })

	return ranked
}

// how far back the frontend can scroll. tick reports are discarded each tick,
// so retention lives here rather than in the report.
const recentPathChangeLimit = 64

type PathChangeStore struct {
	mu      sync.RWMutex
	changes []PCETickReportReroute
}

func NewPathChangeStore() *PathChangeStore {
	return &PathChangeStore{changes: make([]PCETickReportReroute, 0, recentPathChangeLimit)}
}

func (s *PathChangeStore) Record(changes ...PCETickReportReroute) {
	if s == nil || len(changes) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.changes = append(changes, s.changes...)
	if len(s.changes) > recentPathChangeLimit {
		s.changes = s.changes[:recentPathChangeLimit]
	}
}

// Recent returns the newest changes first.
func (s *PathChangeStore) Recent() []PCETickReportReroute {
	if s == nil {
		return []PCETickReportReroute{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]PCETickReportReroute, len(s.changes))
	copy(out, s.changes)

	return out
}

func (r *PCETickReport) Log(ctx context.Context) {
	reroutes := make([]slog.Attr, len(r.Reroutes))
	for i, rr := range r.Reroutes {
		reroutes[i] = slog.Attr{
			Key: fmt.Sprintf("%s>%s", rr.Src, rr.Dst),
			Value: slog.GroupValue(
				slog.Any("from", &rr.FromPath),
				slog.Any("to", &rr.ToPath),
				slog.String("kind", string(rr.Kind)),
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
				slog.String("kind", string(rr.Kind)),
			),
		}
	}

	r.Logger.LogAttrs(ctx, slog.LevelInfo, "pce tick report",
		slog.Time("timestamp", r.Timestamp),
		slog.Attr{Key: "reroutes", Value: slog.GroupValue(reroutes...)},
		slog.Attr{Key: "gated_reroutes", Value: slog.GroupValue(gated...)},
	)
}
