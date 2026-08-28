package dataplane

import (
	"encoding/json"
	"fmt"
	"time"
)

type NodeType string

const (
	NodeTypeCPE NodeType = "cpe"
	NodeTypePE  NodeType = "pe"
)

type NodeIntent struct {
	NodeType   NodeType  `json:"node_type"`
	Intent     Intent    `json:"intent"`
	Timestamp  time.Time `json:"timestamp"`
	Generation uint32    `json:"generation"`
	Version    uint32    `json:"version"`
}

// the wire form. json.RawMessage exists only here: NodeIntent.Intent is a typed
// value everywhere else, so nothing outside this file decodes an intent.
type nodeIntentWire struct {
	NodeType   NodeType        `json:"node_type"`
	Intent     json.RawMessage `json:"intent"`
	Timestamp  time.Time       `json:"timestamp"`
	Generation uint32          `json:"generation"`
	Version    uint32          `json:"version"`
}

// Clone is on the interface so a new variant cannot forget to deep copy
// whatever it holds.
type Intent interface {
	isIntent()
	Clone() Intent
}

// MarshalJSON is not needed: the Intent interface marshals its concrete value,
// so the encoded shape is identical to the wire struct above.
func (n *NodeIntent) UnmarshalJSON(b []byte) error {
	var w nodeIntentWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}

	n.NodeType = w.NodeType
	n.Timestamp = w.Timestamp
	n.Generation = w.Generation
	n.Version = w.Version

	switch w.NodeType {
	case NodeTypeCPE:
		var i CPEIntent
		if err := json.Unmarshal(w.Intent, &i); err != nil {
			return fmt.Errorf("decode cpe intent: %w", err)
		}
		n.Intent = &i
	case NodeTypePE:
		var i PEIntent
		if err := json.Unmarshal(w.Intent, &i); err != nil {
			return fmt.Errorf("decode pe intent: %w", err)
		}
		n.Intent = &i
	default:
		return fmt.Errorf("unknown node type %q", w.NodeType)
	}

	return nil
}
