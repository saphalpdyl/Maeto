package controlplane

import (
	"fmt"
	"net/netip"
	"slices"
	"testing"
)

func segmentsFixture(endXByEdge map[EdgeID]string) (*PCEGraph, *Path) {
	locators := map[NodeID]string{
		"A": "fc00:0:1::/48",
		"B": "fc00:0:2::/48",
		"C": "fc00:0:3::/48",
		"G": "fc00:0:7::/48",
	}

	graph := &Graph{nodes: map[NodeID]*Node{}, edges: map[EdgeID]*Edge{}}
	for id, locator := range locators {
		graph.nodes[id] = &Node{ID: id, Locator: netip.MustParsePrefix(locator)}
	}

	hops := [][2]NodeID{{"A", "B"}, {"B", "C"}, {"C", "G"}}
	path := &Path{Nodes: []NodeID{"A", "B", "C", "G"}}

	for _, hop := range hops {
		id := EdgeID(fmt.Sprintf("%s:eth2-%s:eth2", hop[0], hop[1]))
		edge := &Edge{ID: id, Local: hop[0], Remote: hop[1], LocalIface: "eth2", Up: true}
		if sid, ok := endXByEdge[id]; ok {
			edge.EndX = netip.MustParseAddr(sid)
		}

		graph.edges[id] = edge
		path.Edges = append(path.Edges, NewLinkID(edge))
	}

	return NewPCEGraph(graph), path
}

func addrs(values ...string) []netip.Addr {
	out := make([]netip.Addr, len(values))
	for i, v := range values {
		out[i] = netip.MustParseAddr(v)
	}

	return out
}

func Test_TransitSegmentsUseEndXWhereReported(t *testing.T) {
	graph, path := segmentsFixture(map[EdgeID]string{
		"A:eth2-B:eth2": "fc00:0:1:2::",
		"B:eth2-C:eth2": "fc00:0:2:2::",
		"C:eth2-G:eth2": "fc00:0:3:2::",
	})

	segments, err := transitSegments(graph, path)
	if err != nil {
		t.Fatal(err)
	}

	want := addrs("fc00:0:2:2::", "fc00:0:3:2::")
	if !slices.Equal(segments, want) {
		t.Errorf("want %v, got %v; the headend's own End.X must never appear", want, segments)
	}
}

func Test_TransitSegmentsFallBackToEndWithoutEndX(t *testing.T) {
	graph, path := segmentsFixture(map[EdgeID]string{
		"C:eth2-G:eth2": "fc00:0:3:2::",
	})

	segments, err := transitSegments(graph, path)
	if err != nil {
		t.Fatal(err)
	}

	want := addrs("fc00:0:2::", "fc00:0:3:2::")
	if !slices.Equal(segments, want) {
		t.Errorf("want %v, got %v", want, segments)
	}
}

func Test_TransitSegmentsFallBackToEndWhenLinkIsGone(t *testing.T) {
	graph, path := segmentsFixture(map[EdgeID]string{
		"B:eth2-C:eth2": "fc00:0:2:2::",
	})
	delete(graph.edges, path.Edges[1])

	segments, err := transitSegments(graph, path)
	if err != nil {
		t.Fatal(err)
	}

	want := addrs("fc00:0:2::", "fc00:0:3::")
	if !slices.Equal(segments, want) {
		t.Errorf("want %v, got %v", want, segments)
	}
}

func Test_TransitSegmentsSingleHopHasNoTransitSIDs(t *testing.T) {
	graph, path := segmentsFixture(nil)
	path.Nodes = path.Nodes[:2]
	path.Edges = path.Edges[:1]

	segments, err := transitSegments(graph, path)
	if err != nil {
		t.Fatal(err)
	}

	if len(segments) != 0 {
		t.Errorf("A->B should only need B's DT46, got %v", segments)
	}
}
