package maetoagent

import (
	"context"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/statekv"
)

type stateReporter struct {
	publisher *statekv.Publisher
	key       string
	nodeID    string
}

func (s *stateReporter) Report(ctx context.Context, state *dataplane.NodeState) error {
	if state.NodeID == "" {
		state.NodeID = s.nodeID
	}

	_, err := s.publisher.Publish(ctx, s.key, state)

	return err
}
