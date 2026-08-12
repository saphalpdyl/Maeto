package controlplane

import "testing"

func TestBasePathExtraction(t *testing.T) {
	// mgr := NewClabTopologyManager(ClabTopologyConfig{
	// 	StatePath:       "testdata/latest.json",
	// 	TopologyDirPath: "testdata/",
	// })

	basePath, err := readLatestTopologyBasePath("testdata/latest.json")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if basePath != "0f075f4b4b42" {
		t.Fatalf("invalid extracted base path: expected %s, got %s", "0f075f4b4b42", basePath)
	}
}

func TestLatestTopologyParse(t *testing.T) {
	topoData, err := readLatestTopology("testdata/latest.json", "testdata/")
	if err != nil {
		t.Fatalf("failed to read latest topology: %v", err)
	}

	if len(topoData.Pops) != 8 {
		t.Fatalf("unexpected number of pops: expected %d, got %d", 8, len(topoData.Pops))
	}

	if len(topoData.Links) != 20 {
		t.Fatalf("unexpected number of links: expected %d, got %d", 20, len(topoData.Links))
	}
}

func TestGenerateGraph(t *testing.T) {
	topoData, err := readLatestTopology("testdata/latest.json", "testdata/")
	if err != nil {
		t.Fatalf("failed to read latest topology: %v", err)
	}

	g := generateGraphFromRawTopology(topoData)
	if g == nil {
		t.Fatalf("failed to generated graph from raw topology, is nil")
	}

	if len(g.edges) != 36 {
		t.Fatalf("expected %d edges, got %d", 18, len(g.edges))
	}

	t.Logf("generated graph: %v", g)
}
