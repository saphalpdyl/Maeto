package controlplane

import (
	"context"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/saphalpdyl/maeto/libs/nodesync"
)

func newEndXFixture() *ClabTopologyManager {
	return &ClabTopologyManager{
		graph: &Graph{
			edges: map[EdgeID]*Edge{
				"A:eth2-B:eth2": {ID: "A:eth2-B:eth2", Local: "A", LocalIface: "eth2"},
				"A:eth3-B:eth3": {ID: "A:eth3-B:eth3", Local: "A", LocalIface: "eth3"},
				"B:eth2-A:eth2": {ID: "B:eth2-A:eth2", Local: "B", LocalIface: "eth2"},
			},
		},
	}
}

func Test_SetEndXSIDsMatchesByLocalInterface(t *testing.T) {
	topology := newEndXFixture()
	sid := netip.MustParseAddr("fc00:0:1:2::")

	changed := topology.SetEndXSIDs("A", map[string]netip.Addr{"eth2": sid})

	if len(changed) != 1 || changed[0] != "A:eth2-B:eth2" {
		t.Fatalf("expected only A:eth2-B:eth2 to change, got %v", changed)
	}

	if topology.graph.edges["A:eth2-B:eth2"].EndX != sid {
		t.Errorf("A:eth2 should carry %s", sid)
	}

	if topology.graph.edges["A:eth3-B:eth3"].EndX.IsValid() {
		t.Errorf("A:eth3 was not reported and should have no SID")
	}

	if topology.graph.edges["B:eth2-A:eth2"].EndX.IsValid() {
		t.Errorf("an edge of another node must not pick up A's SID")
	}
}

func Test_SetEndXSIDsClearsInterfacesMissingFromTheReport(t *testing.T) {
	topology := newEndXFixture()
	topology.SetEndXSIDs("A", map[string]netip.Addr{"eth2": netip.MustParseAddr("fc00:0:1:2::")})

	changed := topology.SetEndXSIDs("A", nil)

	if len(changed) != 1 {
		t.Fatalf("expected one edge to be cleared, got %v", changed)
	}

	if topology.graph.edges["A:eth2-B:eth2"].EndX.IsValid() {
		t.Errorf("A:eth2 should have lost its SID")
	}
}

func Test_StaleTopologyReportClearsEndXSIDs(t *testing.T) {
	topology := newEndXFixture()
	collector := NewTopologyReportCollector(nil, slog.New(slog.DiscardHandler), topology)

	collector.apply(context.Background(), nodesync.TopologyReport{
		NodeID:     "A",
		ReportedAt: time.Now().Add(-2 * topologyReportStaleAfter),
		Adjacencies: []nodesync.AdjacencyReport{
			{Interface: "eth2", EndX: netip.MustParseAddr("fc00:0:1:2::")},
		},
	})

	if !topology.graph.edges["A:eth2-B:eth2"].EndX.IsValid() {
		t.Fatalf("report should have set the SID before the sweep")
	}

	collector.clearStale(context.Background())

	if topology.graph.edges["A:eth2-B:eth2"].EndX.IsValid() {
		t.Errorf("stale report should have cleared the SID")
	}
}
