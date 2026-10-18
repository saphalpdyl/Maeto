package controlplane

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

func (c *Controller) stagePeerIntents(tx *RegistryTx, ctx context.Context) {
	// Set peers
	// TODO: Later peer updates must caught from TED update
	g := c.topology.Graph()
	for _, n := range g.nodes {
		peers := make(map[string]nodesync.PeerIntent)

		// Find edges related to this node
		for _, e := range g.edges {
			if e.Local != n.ID {
				continue
			}

			remoteNode, exists := g.nodes[e.Remote]
			if !exists {
				c.logger.WarnContext(ctx, "failed to find remote node", log.NodeID(string(e.Remote)))
				continue
			}

			peerPrefix, err := netip.ParsePrefix(e.RemoteAddr)
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to parse remote node address", log.NodeID(string(e.Remote)))
				continue
			}

			peerIntent := nodesync.PeerIntent{
				PeerID:         string(e.Remote),
				PeerLoopback:   remoteNode.Loopback,
				LocalLoopback:  n.Loopback,
				PeerInterface:  e.RemoteIface,
				LocalInterface: e.LocalIface,
				TelemetryKey:   fmt.Sprintf("%s:%s-%s:%s", string(n.ID), e.LocalIface, string(remoteNode.ID), e.RemoteIface),
				PeerAddress:    peerPrefix.Addr(),
			}

			peers[e.LocalIface] = peerIntent
		}

		c.serviceRegistry.StagePeersForPE(tx, n.ID, peers)
	}
}

func (c *Controller) stageTrafficClassMapping(tx *RegistryTx) {
	g := c.topology.Graph()

	trafficClassIntents := make([]nodesync.PETrafficClassIntent, 0)
	for _, class := range c.tenants.Classes() {
		trafficClassIntents = append(trafficClassIntents, nodesync.PETrafficClassIntent{
			Color: class.Label,
			DSCP:  class.Match.DSCP,
		})
	}

	for nodeID := range g.nodes {
		c.serviceRegistry.StageTrafficClassMappingForPE(tx, nodeID, trafficClassIntents)
	}
}
