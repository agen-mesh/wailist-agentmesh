package engine

import (
	"encoding/json"
	"sync"
)

type RunContext struct {
	mu      sync.RWMutex
	outputs map[string]any
	// order records node IDs in Set() call order. Message()'s "most recent
	// output" used to be read straight off rc.outputs, a Go map whose
	// iteration order is randomized -- harmless while every result was a
	// short sentinel string, but wrong as soon as a node's output actually
	// matters (e.g. a connector read returning real data). This makes
	// "most recent" a real, deterministic fact.
	order []string
	input any
	runID string
}

func NewRunContext(runID string, inputJSON []byte) *RunContext {
	var input any
	if len(inputJSON) > 0 {
		json.Unmarshal(inputJSON, &input)
	}
	return &RunContext{
		outputs: make(map[string]any),
		input:   input,
		runID:   runID,
	}
}

func (rc *RunContext) Set(nodeID string, value any) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if _, exists := rc.outputs[nodeID]; !exists {
		rc.order = append(rc.order, nodeID)
	}
	rc.outputs[nodeID] = value
}

func (rc *RunContext) Get(nodeID string) (any, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	v, ok := rc.outputs[nodeID]
	return v, ok
}

// UserInput returns the original trigger input — always the human's message.
func (rc *RunContext) UserInput() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return anyToString(rc.input)
}

// ToolOutputs returns all outputs keyed by nodeID, excluding the trigger output.
func (rc *RunContext) ToolOutputs() map[string]any {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	out := make(map[string]any, len(rc.outputs))
	for k, v := range rc.outputs {
		out[k] = v
	}
	return out
}

// Message returns the most recent string output for use as LLM user message.
// Kept for backwards compatibility with non-agent nodes.
//
// KNOWN ISSUE: "most recent" means last call to Set(), and runner.go runs
// every node in the same topological level concurrently in its own
// goroutine (see the wg.Add/go func loop in Run) -- so when two sibling
// nodes in the same level both call Set(), which one's output Message()/
// LastOutput() returns is decided by goroutine scheduling, not by the
// workflow graph. A downstream node fed by two parallel upstream branches
// can see either branch's output on any given run of the identical
// workflow. Fixing this properly means Message()/LastOutput() resolving
// the CALLING node's actual flow-edge predecessor rather than "whatever was
// set last" -- which needs the predecessor's node ID threaded through
// RunContexter into every one of the connector call sites that read
// Message()/LastOutput() (20+ across nodes/connectors_*.go), not a change
// local to this file. Left undone here; only genuinely single-predecessor
// chains (the common case in practice) are unaffected.
func (rc *RunContext) Message() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if len(rc.order) == 0 {
		return anyToString(rc.input)
	}
	last := rc.outputs[rc.order[len(rc.order)-1]]
	return anyToString(last)
}

// LastOutput returns the most recent output's raw value, unlike Message()
// which always flattens it to a string. A connector's message template
// (see nodes.expandTemplate) needs the real structure to pick one field out
// of it -- e.g. {{ result.extract }} against a JSON API response -- which a
// pre-stringified value can't offer.
func (rc *RunContext) LastOutput() any {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if len(rc.order) == 0 {
		return rc.input
	}
	return rc.outputs[rc.order[len(rc.order)-1]]
}

func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]any); ok {
		if msg, ok := m["message"].(string); ok {
			return msg
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}
