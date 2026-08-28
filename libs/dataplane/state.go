package dataplane

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

type ResourceState struct {
	Kind Kind            `json:"kind"`
	Key  string          `json:"key"`
	Spec json.RawMessage `json:"spec"`
}

type NodeState struct {
	NodeID     string          `json:"node_id"`
	NodeType   NodeType        `json:"node_type"`
	ReportedAt time.Time       `json:"reported_at"`
	Generation uint32          `json:"generation"`
	Converged  bool            `json:"converged"`
	Passes     int             `json:"passes"`
	Error      string          `json:"error,omitempty"`
	Current    []ResourceState `json:"current"`
	Desired    []ResourceState `json:"desired"`
	Add        []ResourceState `json:"add"`
	Remove     []ResourceState `json:"remove"`
}

type StateReporter interface {
	Report(ctx context.Context, state *NodeState) error
}

func NewResourceState(res Resource) (ResourceState, error) {
	id := res.ID()

	spec, err := json.Marshal(res)
	if err != nil {
		return ResourceState{}, fmt.Errorf("marshal %s resource %s: %w", id.Kind, id.Key, err)
	}

	return ResourceState{Kind: id.Kind, Key: id.Key, Spec: spec}, nil
}

func ResourceStates(resources []Resource) ([]ResourceState, error) {
	out := make([]ResourceState, 0, len(resources))
	for _, res := range resources {
		state, err := NewResourceState(res)
		if err != nil {
			return nil, err
		}

		out = append(out, state)
	}

	sortResourceStates(out)

	return out, nil
}

func ResourceStatesFromMap(resources map[string]Resource) ([]ResourceState, error) {
	return ResourceStates(slices.Collect(maps.Values(resources)))
}

func sortResourceStates(states []ResourceState) {
	slices.SortFunc(states, func(a, b ResourceState) int {
		if a.Kind != b.Kind {
			return strings.Compare(string(a.Kind), string(b.Kind))
		}

		return strings.Compare(a.Key, b.Key)
	})
}
