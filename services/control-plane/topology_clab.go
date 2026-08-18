package controlplane

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type ContainerlabTopologyState struct {
	TopologyHash     string `json:"topology_sha256"`
	GeneratedAt      string `json:"generated_at"`
	GeneratorVersion string `json:"generator_version"`
	Output           string `json:"output"`
}

type RawTopologyData struct {
	Name             string `json:"name"`
	TopologySha256   string `json:"topology_sha256"`
	GeneratorVersion string `json:"generator_version"`
	Defaults         struct {
		LocatorPrefix string `json:"locator_prefix"`
		LinkPrefix    string `json:"link_prefix"`
		EdgePrefix    string `json:"edge_prefix"`
	} `json:"defaults"`
	Pops []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		ClabLabel  string `json:"clab_label"`
		Index      int    `json:"index"`
		IsisNet    string `json:"isis_net"`
		Locator    string `json:"locator"`
		Loopback   string `json:"loopback"`
		Interfaces []struct {
			Name    string `json:"name"`
			Role    string `json:"role"`
			Peer    string `json:"peer"`
			Address string `json:"address"`
		} `json:"interfaces"`
		// null on pops with no cpes, so a pointer distinguishes "no access side"
		Access *struct {
			Interface string `json:"interface"`
			Address   string `json:"address"`
			Aggregate string `json:"aggregate"`
			Nexthop   string `json:"nexthop"`
		} `json:"access"`
	} `json:"pops"`
	Cpes []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ClabLabel     string `json:"clab_label"`
		Instance      int    `json:"instance"`
		Attach        string `json:"attach"`
		AttachNode    string `json:"attach_node"`
		TransitNode   string `json:"transit_node"`
		Subnet        string `json:"subnet"`
		Address       string `json:"address"`
		Gateway       string `json:"gateway"`
		Interface     string `json:"interface"`
		PeerInterface string `json:"peer_interface"`
	} `json:"cpes"`
	Links []struct {
		Index    int    `json:"index"`
		Type     string `json:"type"`
		Instance int    `json:"instance"`
		Subnet   string `json:"subnet"`
		A        struct {
			Kind      string `json:"kind"`
			ID        string `json:"id"`
			Node      string `json:"node"`
			Interface string `json:"interface"`
			Address   string `json:"address"`
		} `json:"a"`
		B struct {
			Kind      string `json:"kind"`
			ID        string `json:"id"`
			Node      string `json:"node"`
			Interface string `json:"interface"`
			Address   string `json:"address"`
		} `json:"b"`
	} `json:"links"`
}

func readLatestTopologyBasePath(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec
	if err != nil {
		return "", err
	}

	var state ContainerlabTopologyState
	if err = json.Unmarshal(data, &state); err != nil {
		return "", err
	}

	baseTopologyPath := filepath.Base(state.Output)
	return baseTopologyPath, nil
}

func readLatestTopology(statePath string, topologyDirPath string) (*RawTopologyData, error) {
	// Will give us path like "0f075f4b4b42"
	topologyBasePath, err := readLatestTopologyBasePath(statePath)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(filepath.Join(topologyDirPath, topologyBasePath, TOPOLOGY_DATA_FILENAME)) // #nosec
	if err != nil {
		return nil, err
	}

	var topologyData RawTopologyData
	if err = json.Unmarshal(raw, &topologyData); err != nil {
		return nil, err
	}

	return &topologyData, nil
}

type ClabTopologyConfig struct {
	StatePath       string
	TopologyDirPath string
}

type ClabTopologyManager struct {
	mu    sync.RWMutex
	ready bool
	graph *Graph

	config ClabTopologyConfig
}

var _ TopologyManager = (*ClabTopologyManager)(nil)

func NewClabTopologyManager(cfg ClabTopologyConfig) *ClabTopologyManager {
	return &ClabTopologyManager{
		config: cfg,
		ready:  false,
		graph:  nil,
	}
}

func generateGraphFromRawTopology(rawTopoData *RawTopologyData) *Graph {
	g := &Graph{
		nodes:    make(map[NodeID]*Node),
		edges:    make(map[EdgeID]*Edge),
		prefixes: make(map[string]*Prefix),
		adj:      make(map[NodeID][]EdgeID),
	}

	for _, p := range rawTopoData.Pops {
		g.nodes[NodeID(p.ID)] = &Node{
			ID:       NodeID(p.ID),
			Name:     p.Name,
			ASN:      0,
			ISISNet:  "",
			Loopback: p.Loopback,
			Locator:  p.Locator,
			Attrs:    map[string]string{},
		}
	}

	for _, l := range rawTopoData.Links {
		if l.Type != "core" {
			continue
		}

		edgeId := EdgeID(fmt.Sprintf("%s:%s-%s:%s", l.A.ID, l.A.Interface, l.B.ID, l.B.Interface))
		g.edges[edgeId] = &Edge{
			ID:          edgeId,
			Local:       NodeID(l.A.ID),
			Remote:      NodeID(l.B.ID),
			Role:        "link",
			LocalIface:  l.A.Interface,
			RemoteIface: l.B.Interface,
			LocalAddr:   l.A.Address,
			RemoteAddr:  l.B.Address,
			Subnet:      l.Subnet,
			Metric:      0,
			TEMetric:    0,
			Bandwidth:   0,
			Delay:       0,
			Up:          true,
		}

		revEdgeId := EdgeID(fmt.Sprintf("%s:%s-%s:%s", l.B.ID, l.B.Interface, l.A.ID, l.A.Interface))
		g.edges[revEdgeId] = &Edge{
			ID:          revEdgeId,
			Local:       NodeID(l.B.ID),
			Remote:      NodeID(l.A.ID),
			Role:        "link",
			LocalIface:  l.B.Interface,
			RemoteIface: l.A.Interface,
			LocalAddr:   l.B.Address,
			RemoteAddr:  l.A.Address,
			Subnet:      l.Subnet,
			Metric:      0,
			TEMetric:    0,
			Bandwidth:   0,
			Delay:       0,
			Up:          true,
		}

		g.adj[NodeID(l.A.ID)] = append(g.adj[NodeID(l.A.ID)], edgeId)
		g.adj[NodeID(l.B.ID)] = append(g.adj[NodeID(l.B.ID)], revEdgeId)
	}

	return g
}

func (c *ClabTopologyManager) LoadTopology() error {
	rawTopoData, err := readLatestTopology(c.config.StatePath, c.config.TopologyDirPath)
	if err != nil {
		return fmt.Errorf("failed to open and parse containerlab topology: %w, statepath: %s, topologydirpath: %s", err, c.config.StatePath, c.config.TopologyDirPath)
	}

	graph := generateGraphFromRawTopology(rawTopoData)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.graph = graph
	c.ready = true

	return nil
}

func (c *ClabTopologyManager) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.ready
}

func (c *ClabTopologyManager) Graph() *Graph {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.graph
}

func (c *ClabTopologyManager) GetNodeByID(nodeID NodeID) (*Node, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	node, exists := c.graph.nodes[nodeID]
	return node, exists
}
