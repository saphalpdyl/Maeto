package controlplane

import (
	"testing"
)

func loadTestGraph(t *testing.T) *Graph {
	t.Helper()

	raw, err := readLatestTopology("testdata/latest.json", "testdata/")
	if err != nil {
		t.Fatalf("read topology: %v", err)
	}

	graph, err := generateGraphFromRawTopology(raw)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}

	return graph
}

func TestSnapshotDropsMirroredEdges(t *testing.T) {
	graph := loadTestGraph(t)
	snapshot := SnapshotTopology(graph, SRv6DomainMetadata{}, nil, nil)

	if len(graph.edges) != 2*len(snapshot.Edges) {
		t.Fatalf("expected half of %d graph edges, got %d", len(graph.edges), len(snapshot.Edges))
	}

	seen := make(map[string]string, len(snapshot.Edges))
	for _, edge := range snapshot.Edges {
		local := edge.Local + ":" + edge.LocalIface
		remote := edge.Remote + ":" + edge.RemoteIface

		key := local + "|" + remote
		if remote < local {
			key = remote + "|" + local
		}

		if first, dup := seen[key]; dup {
			t.Fatalf("link %s emitted twice: %s and %s", key, first, edge.ID)
		}
		seen[key] = edge.ID
	}
}

func TestSnapshotEdgesAreStableAcrossRuns(t *testing.T) {
	graph := loadTestGraph(t)

	first := SnapshotTopology(graph, SRv6DomainMetadata{}, nil, nil)

	for range 20 {
		next := SnapshotTopology(graph, SRv6DomainMetadata{}, nil, nil)

		if len(next.Edges) != len(first.Edges) {
			t.Fatalf("edge count changed: %d then %d", len(first.Edges), len(next.Edges))
		}

		for i := range next.Edges {
			if next.Edges[i].ID != first.Edges[i].ID {
				t.Fatalf("edge %d changed between runs: %s then %s",
					i, first.Edges[i].ID, next.Edges[i].ID)
			}
		}
	}
}

func TestSnapshotPCEEdgesCoverEveryLinkOnceInItsOwnDirection(t *testing.T) {
	graph := loadTestGraph(t)
	snapshot := SnapshotTopology(graph, SRv6DomainMetadata{}, nil, nil)

	owner := make(map[string]string)
	for _, pceEdge := range snapshot.PCEEdges {
		for _, member := range pceEdge.Members {
			if previous, taken := owner[member]; taken {
				t.Fatalf("link %s is in both %s and %s", member, previous, pceEdge.ID)
			}
			owner[member] = pceEdge.ID.String()

			edge := graph.edges[EdgeID(member)]
			if string(edge.Local) != pceEdge.From || string(edge.Remote) != pceEdge.To {
				t.Fatalf("link %s runs %s->%s but sits in %s->%s", member, edge.Local, edge.Remote, pceEdge.From, pceEdge.To)
			}
		}
	}

	for id, edge := range graph.edges {
		if _, covered := owner[string(id)]; edge.Up && !covered {
			t.Fatalf("up link %s is missing from the pce edges", id)
		}
	}
}
