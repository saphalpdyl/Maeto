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
	snapshot := SnapshotTopology(graph, SRv6DomainMetadata{})

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

	first := SnapshotTopology(graph, SRv6DomainMetadata{})

	for range 20 {
		next := SnapshotTopology(graph, SRv6DomainMetadata{})

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
