package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/saphalpdyl/maeto/libs/store"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

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
	PathChangeHeldDown PathChangeKind = "held_down"
	PathChangeNew      PathChangeKind = "new"
	PathChangeLinkDown PathChangeKind = "link_down"
)

type PCETickReportReroute struct {
	At     time.Time `json:"at"`
	Tenant TenantID  `json:"tenant"`
	Label  uint64    `json:"label"`

	Src  NodeID         `json:"src"`
	Dst  NodeID         `json:"dst"`
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
func attributeCauses(from *Path, dim CostDimension, was, now map[CostGraphEdge]*Cost) []PathChangeCause {
	if from == nil || len(was) == 0 {
		return nil
	}

	causes := make([]PathChangeCause, 0, len(from.Edges))
	total := 0.0

	for i := 0; i+1 < len(from.Nodes); i++ {
		for key, prev := range CostsBetween(was, from.Nodes[i], from.Nodes[i+1]) {
			curr, hasCurr := now[key]
			if !hasCurr || prev == nil || curr == nil {
				continue
			}

			delta := curr.Costs[dim] - prev.Costs[dim]
			if delta <= 0 {
				continue
			}

			total += delta
			causes = append(causes, PathChangeCause{
				Edge:  key.ID,
				Dim:   dim,
				Was:   prev.Costs[dim],
				Now:   curr.Costs[dim],
				Delta: delta,
			})
		}
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

const pathChangeWriteTimeout = 5 * time.Second

type PathChangeStore struct {
	mu      sync.RWMutex
	changes []PCETickReportReroute

	db     *store.DataStore
	logger *slog.Logger
}

func NewPathChangeStore(db *store.DataStore, logger *slog.Logger) *PathChangeStore {
	return &PathChangeStore{
		changes: make([]PCETickReportReroute, 0, recentPathChangeLimit),
		db:      db,
		logger:  logger,
	}
}

func (s *PathChangeStore) Record(ctx context.Context, changes ...PCETickReportReroute) {
	if s == nil || len(changes) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.changes = append(changes, s.changes...)
	if len(s.changes) > recentPathChangeLimit {
		s.changes = s.changes[:recentPathChangeLimit]
	}

	if s.db != nil {
		go s.persist(context.WithoutCancel(ctx), changes)
	}
}

func (s *PathChangeStore) persist(ctx context.Context, changes []PCETickReportReroute) {
	ctx, cancel := context.WithTimeout(ctx, pathChangeWriteTimeout)
	defer cancel()

	for _, c := range changes {
		causes := c.Causes
		if causes == nil {
			causes = []PathChangeCause{}
		}

		causesJSON, err := json.Marshal(causes)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to encode path change causes", log.Err(err))
			continue
		}

		err = s.db.Queries.InsertPathChange(ctx, store.InsertPathChangeParams{
			Time:      pgtype.Timestamptz{Time: c.At, Valid: true},
			Tenant:    int64(c.Tenant),
			Label:     int64(c.Label),
			Src:       string(c.Src),
			Dst:       string(c.Dst),
			Kind:      string(c.Kind),
			FromNodes: nodeNames(c.FromPath.Nodes),
			ToNodes:   nodeNames(c.ToPath.Nodes),
			FromCost:  pgtype.Float8{Float64: c.FromPath.Cost, Valid: len(c.FromPath.Nodes) > 0},
			ToCost:    pgtype.Float8{Float64: c.ToPath.Cost, Valid: len(c.ToPath.Nodes) > 0},
			Causes:    causesJSON,
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to record path change", log.Err(err))
		}
	}
}

func nodeNames(nodes []NodeID) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, string(node))
	}

	return names
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
			Key: fmt.Sprintf("%d:%s>%s", rr.Tenant, rr.Src, rr.Dst),
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
			Key: fmt.Sprintf("%d:%s>%s", rr.Tenant, rr.Src, rr.Dst),
			Value: slog.GroupValue(
				slog.Any("from", &rr.FromPath),
				slog.Any("to", &rr.ToPath),
				slog.String("kind", string(rr.Kind)),
			),
		}
	}

	r.Logger.LogAttrs(ctx, slog.LevelDebug, "pce tick report",
		slog.Time("timestamp", r.Timestamp),
		slog.Attr{Key: "reroutes", Value: slog.GroupValue(reroutes...)},
		slog.Attr{Key: "gated_reroutes", Value: slog.GroupValue(gated...)},
	)
}
