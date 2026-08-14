package nodes

// RunContexter interface satisfied by engine.RunContext via duck typing.
// We use a local interface to avoid circular import engine → nodes → engine.
type RunContexter interface {
	Message() string
	UserInput() string
	ToolOutputs() map[string]any
	Set(string, any)
	Get(string) (any, bool)
	// OutputOrder returns node IDs oldest-first. {{ node.<id> }} references
	// resolve via Get(id), a direct keyed lookup that needs no ordering —
	// this exists for callers that need the insertion sequence itself (e.g.
	// engine.RunContext.Message(), which reads the tail of this same order).
	OutputOrder() []string
}

// OutputOrder for emptyRunContext (type defined in the frozen tendril.go) is
// added here — not in tendril.go — so the frozen file's bytes never change.
// Nil is correct: emptyRunContext has no addressable upstream outputs.
func (emptyRunContext) OutputOrder() []string { return nil }
