package engine

import (
	"context"
	"sync"
)

// runRegistry tracks the cancel function for each in-flight workflow run,
// keyed by workflow ID. Only the most recent run per workflow is tracked;
// registering a new run cancels any previous one.
type runRegistry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func newRunRegistry() *runRegistry {
	return &runRegistry{cancels: make(map[string]context.CancelFunc)}
}

func (reg *runRegistry) register(workflowID string, cancel context.CancelFunc) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if old, ok := reg.cancels[workflowID]; ok {
		old()
	}
	reg.cancels[workflowID] = cancel
}

func (reg *runRegistry) cancel(workflowID string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	fn, ok := reg.cancels[workflowID]
	if ok {
		fn()
	}
	return ok
}

func (reg *runRegistry) deregister(workflowID string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	delete(reg.cancels, workflowID)
}
