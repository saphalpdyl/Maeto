// Package maetoagent status_reporter reports the dataplane lane of maeto-node-state, used by the UI
// Internally used state such as local SID allocation and link states(in the future)
//
//	should not go through this path
package maetoagent

import (
	"context"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
)

type stateReporter struct {
	publisher *nodesync.Publisher
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
