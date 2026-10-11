package controlplane

import (
	"fmt"
	"net/netip"
)

// transitSegments gives one SID per transit node of path: End.X out of the
// path's link when the node has reported one, End otherwise. The headend gets
// no SID because Linux cannot execute its own SIDs, so the first hop is plain
// routing towards the second node. The egress is reached by the DT46 the
// caller appends.
func transitSegments(graph *PCEGraph, path *Path) ([]netip.Addr, error) {
	if len(path.Edges) != len(path.Nodes)-1 {
		return nil, fmt.Errorf("path has %d nodes but %d edges", len(path.Nodes), len(path.Edges))
	}

	segments := make([]netip.Addr, 0, len(path.Nodes))

	for i := 1; i < len(path.Nodes)-1; i++ {
		node, exists := graph.nodes[path.Nodes[i]]
		if !exists {
			return nil, fmt.Errorf("transit node %s is not in the topology", path.Nodes[i])
		}

		endX, ok := endXOf(graph, path.Edges[i])
		if ok {
			segments = append(segments, endX)
			continue
		}

		segments = append(segments, node.Locator.Addr())
	}

	return segments, nil
}

// endXOf only answers for a single link that is still in the graph. A bundle,
// a link that went down, or a node that has not reported yet falls back to End,
// which lets the IGP pick the way out.
func endXOf(graph *PCEGraph, id PCEEdgeID) (netip.Addr, bool) {
	edge, exists := graph.edges[id]
	if !exists || edge.IsBundle() || len(edge.RealEdges) != 1 {
		return netip.Addr{}, false
	}

	endX := edge.RealEdges[0].EndX

	return endX, endX.IsValid()
}
