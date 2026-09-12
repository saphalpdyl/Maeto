// debug_devtools.go
//go:build devtools

package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/nats-io/nats.go"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

const costGraphOverrideSubject = "maeto.debug.tools.cost_graph.override"

type CostGraphOverrideOperation string

const (
	CostGraphOverrideUpsert CostGraphOverrideOperation = "COST_GRAPH_OVERRIDE_UPSERT"
	CostGraphOverrideList   CostGraphOverrideOperation = "COST_GRAPH_OVERRIDE_LIST"
)

type CostGraphOverridePayload struct {
	Operation CostGraphOverrideOperation `json:"operation"`

	// Upsert arguments
	EdgeID        *string  `json:"edge_id,omitempty"`
	CostDimension *string  `json:"cost_dimension,omitempty"`
	Cost          *float64 `json:"cost,omitempty"`

	// List filters
	ListFilterEdgeIDs        []string        `json:"list_filter_edge_ids,omitempty"`
	ListFilterCostDimensions []CostDimension `json:"list_filter_cost_dimensions,omitempty"`
}

type CostGraphOverrideEntry struct {
	EdgeID        string   `json:"edge_id"`
	CostDimension string   `json:"cost_dimension"`
	Cost          float64  `json:"cost"`
	PreviousCost  *float64 `json:"previous_cost,omitempty"`
}

type CostGraphOverrideResponse struct {
	Operation CostGraphOverrideOperation `json:"operation"`
	Error     string                     `json:"error,omitempty"`
	Entries   []CostGraphOverrideEntry   `json:"entries"`
}

func (c *Controller) startDebugTools(
	ctx context.Context,
	logger *slog.Logger,
) {
	logger = logger.With(log.NATSSubject(costGraphOverrideSubject))

	s, err := c.js.Conn().Subscribe(costGraphOverrideSubject, func(msg *nats.Msg) {
		data, err := json.Marshal(c.costGraphOverride(ctx, logger, msg.Data))
		if err != nil {
			logger.ErrorContext(ctx, "failed to marshal response", log.Err(err))
			return
		}

		if err := msg.Respond(data); err != nil {
			logger.ErrorContext(ctx, "failed to respond back", log.Err(err))
		}
	})

	if err != nil {
		logger.DebugContext(ctx, "failed to subscribe to debug subject")
		return
	}

	go func() {
		<-ctx.Done()
		if err := s.Drain(); err != nil {
			logger.ErrorContext(ctx, "failed to drain debug subject")
		}
	}()
}

func (c *Controller) costGraphOverride(
	ctx context.Context,
	logger *slog.Logger,
	data []byte,
) CostGraphOverrideResponse {
	var payload CostGraphOverridePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		logger.ErrorContext(ctx, "failed to unmarshal graph override payload", log.Err(err))
		return CostGraphOverrideResponse{
			Error:   fmt.Sprintf("malformed payload: %s", err),
			Entries: []CostGraphOverrideEntry{},
		}
	}

	resp := CostGraphOverrideResponse{
		Operation: payload.Operation,
		Entries:   []CostGraphOverrideEntry{},
	}

	switch payload.Operation {
	case CostGraphOverrideUpsert:
		if payload.EdgeID == nil || payload.CostDimension == nil || payload.Cost == nil {
			logger.ErrorContext(ctx, "invalid upsert payload")
			resp.Error = "upsert needs edge_id, cost_dimension and cost"
			return resp
		}

		c.pce.costGraph.mu.Lock()
		defer c.pce.costGraph.mu.Unlock()

		cost, exists := c.pce.costGraph.graph[EdgeID(*payload.EdgeID)]
		if !exists {
			logger.ErrorContext(ctx, "edge does not exist in cost graph",
				slog.String("edge_id", *payload.EdgeID))
			resp.Error = fmt.Sprintf("edge %s not in cost graph", *payload.EdgeID)
			return resp
		}

		dim := CostDimension(*payload.CostDimension)

		entry := CostGraphOverrideEntry{
			EdgeID:        *payload.EdgeID,
			CostDimension: *payload.CostDimension,
			Cost:          *payload.Cost,
		}
		if prev, set := cost.Costs[dim]; set {
			entry.PreviousCost = &prev
		}

		cost.Costs[dim] = *payload.Cost
		resp.Entries = append(resp.Entries, entry)

		logger.InfoContext(ctx, "cost graph override applied",
			slog.String("edge_id", entry.EdgeID),
			slog.String("cost_dimension", entry.CostDimension),
			slog.Float64("cost", entry.Cost),
		)

	case CostGraphOverrideList:
		c.pce.costGraph.mu.RLock()
		defer c.pce.costGraph.mu.RUnlock()

		for _, edgeID := range slices.Sorted(maps.Keys(c.pce.costGraph.graph)) {
			if len(payload.ListFilterEdgeIDs) > 0 &&
				!slices.Contains(payload.ListFilterEdgeIDs, string(edgeID)) {
				continue
			}

			costs := c.pce.costGraph.graph[edgeID].Costs
			for _, dim := range slices.Sorted(maps.Keys(costs)) {
				if len(payload.ListFilterCostDimensions) > 0 &&
					!slices.Contains(payload.ListFilterCostDimensions, dim) {
					continue
				}

				resp.Entries = append(resp.Entries, CostGraphOverrideEntry{
					EdgeID:        string(edgeID),
					CostDimension: string(dim),
					Cost:          costs[dim],
				})
			}
		}

		logger.DebugContext(ctx, "cost graph listed", slog.Int("entries", len(resp.Entries)))

	default:
		logger.ErrorContext(ctx, "invalid operation",
			slog.String("operation", string(payload.Operation)))
		resp.Error = fmt.Sprintf("invalid operation %q", payload.Operation)
	}

	return resp
}
