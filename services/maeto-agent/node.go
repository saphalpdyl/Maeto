package maetoagent

import (
	"encoding/json"
	"fmt"
	"os"
)

type NodeInterface struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Peer    string `json:"peer"`
	Address string `json:"address"`
}

type NodeAccess struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Aggregate string `json:"aggregate"`
	Nexthop   string `json:"nexthop"`
}

type Node struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Index      int               `json:"index"`
	ISISNet    string            `json:"isis_net"`
	Locator    string            `json:"locator"`
	Loopback   string            `json:"loopback"`
	Interfaces []NodeInterface   `json:"interfaces"`
	Access     *NodeAccess       `json:"access"`
	Data       map[string]string `json:"data"`
}

func LoadNode(path string) (*Node, error) {
	data, err := os.ReadFile(path) // #nosec
	if err != nil {
		return nil, fmt.Errorf("failed to read node file %s: %w", path, err)
	}

	var node Node
	if err = json.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("failed to parse node file %s: %w", path, err)
	}

	if node.ID == "" || node.Name == "" {
		return nil, fmt.Errorf("node file %s is missing id or name", path)
	}

	return &node, nil
}

func (n *Node) HasAccessSide() bool {
	return n.Access != nil
}

func (n *Node) CoreInterfaces() []NodeInterface {
	out := make([]NodeInterface, 0, len(n.Interfaces))
	for _, i := range n.Interfaces {
		if i.Role == "core" {
			out = append(out, i)
		}
	}

	return out
}

func (n *Node) IntentKey() string {
	return fmt.Sprintf("pop.%s", n.ID)
}

func (n *Node) ReportSubject() string {
	return fmt.Sprintf("maeto.agent.pop%s.report", n.ID)
}
