/**
Topology Independent Loop Free Alternate (TI-LFA) [RFC 9855]

This implementation is the most basic of the basic case of TI-LFA
where the repair list is explicitly defined at every hop.

""" RFC 9855 § 3 Base Principle
The basic algorithm to compute the repair path is to pre-compute SPT_new(R, X) and, for each destination, encode the \
repair path as a loop-free segment list. One way to provide a loop-free segment list is to use Adjacency SIDs only.
"""

This implementation ignores all of the P-space Q-space complexity because
it is a stand-in replacement for the actual FRR TI-LFA implementation
that I've been working on the side.

Once that lands, LFA will be removed from the controller (as it should)
and run locally per-P/PE
*/

package controlplane

import (
	"context"
	"fmt"
	"net/netip"
	"slices"

	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

// Given locator fc00:0:5::/48
// Maeto allocates (through FRR) from fc00:0:5:0000::/52 - fc00:0:5:efff::/52 for control SID (End.X, End etc.)
// DT46 allocates from fc00:0:5:0000::/52 - fc00:0:5:ffff::/52
func serviceSIDRange(locator netip.Prefix) netip.Prefix {
	return netip.PrefixFrom(withHextets(locator, locator.Bits(), serviceFuncBase), locator.Bits()+4)
}

func (c *Controller) buildAndStageLFARepairSegments(ctx context.Context, repairList LFARepairList, tx *RegistryTx) {
	topo := c.topology.Graph().Clone()

	for nodeID, nodeRepairList := range repairList.RepairList {
		intentsForNode := make([]nodesync.LFARepairIntent, 0, len(nodeRepairList))

		for _, r := range nodeRepairList {

			// Get triggers i.e packet with slices.Contains(triggers, DA) are repaired
			// Get End towards protected
			protecting, ok := repairList.Graph.nodes[r.Protecting]
			if !ok {
				continue
			}

			triggers := make([]nodesync.LFATrigger, 0)
			triggers = append(triggers, nodesync.LFATrigger{
				SIDPrefix: serviceSIDRange(protecting.Locator),
				Kind:      nodesync.LFATriggerSIDPrefixService,
			}) // End

			for _, edge := range topo.edges {
				if edge.Local != nodeID || edge.Remote != protecting.ID || !edge.LastEndX.IsValid() {
					continue
				}

				triggers = append(triggers, nodesync.LFATrigger{
					SIDPrefix: netip.PrefixFrom(edge.LastEndX, 128),
					Kind:      nodesync.LFATriggerSIDPrefixEndX,
				})
			}

			protector, ok := repairList.Graph.nodes[r.Protector]
			if !ok {
				continue
			}

			var segments []netip.Addr

			if r.IsPLRProtector {
				// PE can process its own End.M SID after it encaps because its forwarded
				// It's only for locally-generated packets that the PE
				// can't process immediately at the headend
				segments = []netip.Addr{protector.MirrorLocator.Addr()}
			} else {
				var err error
				segments, err = lfaRepairSegments(repairList.Graph, &r.Path)
				if err != nil {
					c.logger.ErrorContext(ctx, "failed to build repair list segments", log.Err(err))
					continue
				}

				segments = append(segments, protector.MirrorLocator.Addr())
				slices.Reverse(segments)
			}

			intentsForNode = append(intentsForNode, nodesync.LFARepairIntent{
				Protector:      string(r.Protector),
				Protecting:     string(r.Protecting),
				Triggers:       triggers,
				Segments:       segments,
				IsPLRProtector: r.IsPLRProtector,
			})
		}

		c.serviceRegistry.StageRepairIntentForPE(tx, nodeID, intentsForNode)
	}
}

func lfaRepairSegments(graph *PCEGraph, path *Path) ([]netip.Addr, error) {
	if len(path.Edges) != len(path.Nodes)-1 {
		return nil, fmt.Errorf("path has %d nodes but %d edges", len(path.Nodes), len(path.Edges))
	}

	segments := make([]netip.Addr, 0, len(path.Nodes))

	for i := 1; i < len(path.Nodes)-1; i++ {
		node, exists := graph.nodes[path.Nodes[i]]
		if !exists {
			return nil, fmt.Errorf("transit node %s is not in the topology", path.Nodes[i])
		}

		endX, ok := lfaRepairEndXOf(graph, path.Edges[i])
		if ok {
			segments = append(segments, endX)
			continue
		}

		segments = append(segments, node.Locator.Addr())
	}

	return segments, nil
}

func lfaRepairEndXOf(graph *PCEGraph, id PCEEdgeID) (netip.Addr, bool) {
	edge, exists := graph.edges[id]
	if !exists {
		return netip.Addr{}, false
	}

	endX := edge.RealEdges[0].EndX
	return endX, endX.IsValid()
}
