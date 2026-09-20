// Package nodesync owns the node intent schema and the jetstream kv transport
// that carries it: the control plane publishes intent and reads back observed
// state, the agent and portal watch intent and publish their state.
package nodesync
