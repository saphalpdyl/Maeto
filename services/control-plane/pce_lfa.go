package controlplane

import (
	"context"
	"maps"
	"slices"
)

// NodeID refers to the PLR not the protector or the protectee
type LFARepairList struct {
	RepairList map[NodeID][]LFARepairListItem
	Graph      *PCEGraph // The graph the RepairList was calculated on
}

type LFARepairListItem struct {
	Protector  NodeID // Where the list routes towards
	Protecting NodeID // For whom this node is the PLR for
	Path       Path

	// Deprecated: PLR can never be a protector
	IsPLRProtector bool
}

func (p *PCE) computeTILFASegments(ctx context.Context, topo *Graph, g *PCEGraph) LFARepairList {
	repairList := LFARepairList{
		RepairList: make(map[NodeID][]LFARepairListItem),
		Graph:      NewPCEGraph(topo),
	}

	for _, protectorNode := range g.nodes {
		for _, protects := range protectorNode.ProtectorOf {
			pruned := PruneGraph(g, []NodeID{protects}, nil)

			// If we calculate on PCEGraph which removes the edges and the node
			// the next tick after the link-down event, the PLR will no longer
			// have an adj to the down node, and un-mark itself as the PLR
			// while it's still is working as a PLR since its adjacent to the down node
			// That's why we calculate on *Graph instead of *PCEGraph because it
			// retains the topology information without minding the down events
			plrs := plrsOf(topo, protects)

			for _, plr := range plrs {
				if topo.down[plr] {
					continue
				}

				repairListOfNode, exists := repairList.RepairList[plr]
				if !exists {
					repairListOfNode = make([]LFARepairListItem, 0)
				}

				path, err := p.ComputePath(pruned, plr, protectorNode.ID, WeightByHop())
				if err != nil {
					p.logger.ErrorContext(ctx, "failed to compute repair path", "node_from", plr, "node_to", protectorNode.ID, "err", err)
					continue
				}

				repairListOfNode = append(repairListOfNode, LFARepairListItem{
					Protector:  protectorNode.ID,
					Protecting: protects,
					Path:       *path,
				})

				repairList.RepairList[plr] = repairListOfNode
			}
		}
	}

	return repairList
}

func plrsOf(g *Graph, node NodeID) []NodeID {
	plrNodes := make(map[NodeID]struct{})

	for _, edge := range g.edges {
		if edge.Local == node {
			plrNodes[edge.Remote] = struct{}{}
		} else if edge.Remote == node {
			plrNodes[edge.Local] = struct{}{}
		}
	}

	return slices.Collect(maps.Keys(plrNodes))
}

func PruneGraph(g *PCEGraph, nodes []NodeID, edges []PCEEdgeID) *PCEGraph {
	pruned := &PCEGraph{
		nodes: make(map[NodeID]*Node, len(g.nodes)),
		edges: make(map[PCEEdgeID]*PCEEdge, len(g.edges)),
		adj:   make(map[NodeID][]PCEEdgeID, len(g.adj)),
	}

	for _, node := range g.nodes {
		if !slices.Contains(nodes, node.ID) {
			pruned.nodes[node.ID] = node
		}
	}

	for _, edge := range g.edges {
		remove := false

		for _, e := range edge.RealEdges {
			if !e.Up {
				remove = true
				break
			}
		}

		for _, removeNode := range nodes {
			if edge.NodeFrom == removeNode || edge.NodeTo == removeNode {
				remove = true
				break
			}
		}

		if !remove && !slices.Contains(edges, edge.ID) {
			pruned.edges[edge.ID] = edge
		}
	}

	for n, ids := range g.adj {
		if slices.Contains(nodes, n) {
			// Skip copying the adjs of a removed node
			continue
		}

		kept := slices.DeleteFunc(slices.Clone(ids), func(id PCEEdgeID) bool {
			_, ok := pruned.edges[id]
			return !ok
		})

		if len(kept) > 0 {
			pruned.adj[n] = kept
		}
	}

	return pruned
}
