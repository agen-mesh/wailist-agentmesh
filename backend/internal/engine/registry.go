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
	entries map[string]registryEntry
	nextGen uint64
}

// registryEntry pairs a run's cancel func with the generation token issued
// when it was registered. deregister only removes an entry if the caller's
// token still matches -- without this, a run whose deregister is delayed
// (e.g. behind an up-to-several-minutes deferred settlement call) can fire
// AFTER a same-workflow retrigger has already overwritten the map entry,
// deleting the NEWER run's registration instead of its own. Stop() on that
// newer, still-running run would then silently no-op (registry.cancel
// returns false, "not running") even though it is actively running.
type registryEntry struct {
	cancel context.CancelFunc
	gen    uint64
}

func newRunRegistry() *runRegistry {
	return &runRegistry{entries: make(map[string]registryEntry)}
}

// register returns a generation token the caller must pass to deregister.
func (reg *runRegistry) register(workflowID string, cancel context.CancelFunc) uint64 {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if old, ok := reg.entries[workflowID]; ok {
		old.cancel()
	}
	reg.nextGen++
	gen := reg.nextGen
	reg.entries[workflowID] = registryEntry{cancel: cancel, gen: gen}
	return gen
}

func (reg *runRegistry) cancel(workflowID string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, ok := reg.entries[workflowID]
	if ok {
		e.cancel()
	}
	return ok
}

// deregister removes workflowID's entry only if gen still matches what
// register returned for it -- a stale caller (superseded by a newer
// register call for the same workflowID) is a no-op instead of deleting the
// newer run's entry.
func (reg *runRegistry) deregister(workflowID string, gen uint64) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if e, ok := reg.entries[workflowID]; ok && e.gen == gen {
		delete(reg.entries, workflowID)
	}
}
