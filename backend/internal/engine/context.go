package engine

import (
	"encoding/json"
	"sync"
)

type RunContext struct {
	mu      sync.RWMutex
	outputs map[string]any
	// order records node IDs in the sequence they were Set. Message() reads
	// the tail of this rather than ranging over `outputs` — Go randomizes map
	// iteration order, so the old "most recent" was actually "an arbitrary
	// one", non-deterministic across runs of the same workflow.
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
	// A re-Set moves the node to the tail: it just produced the newest output.
	for i, id := range rc.order {
		if id == nodeID {
			rc.order = append(rc.order[:i], rc.order[i+1:]...)
			break
		}
	}
	rc.order = append(rc.order, nodeID)
	rc.outputs[nodeID] = value
}

// OutputOrder returns node IDs in the order their outputs were Set, oldest
// first. The returned slice is a copy — callers may retain it safely.
func (rc *RunContext) OutputOrder() []string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	out := make([]string, len(rc.order))
	copy(out, rc.order)
	return out
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

// Message returns the most recent node output as a string, falling back to the
// trigger input when nothing has run yet.
//
// LOAD-BEARING FOR PAYMENTS: this value is the request body sent to paid x402
// endpoints (nodes/tool402.go) and the Python source sent to Tendril's metered
// /x402/run (nodes/tendril.go). Changing which output it selects changes what
// real money is spent on. Do not alter the selection rule.
func (rc *RunContext) Message() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if len(rc.order) == 0 {
		return anyToString(rc.input)
	}
	return anyToString(rc.outputs[rc.order[len(rc.order)-1]])
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
