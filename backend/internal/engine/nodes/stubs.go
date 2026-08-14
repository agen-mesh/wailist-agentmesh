package nodes

// RunContexter interface satisfied by engine.RunContext via duck typing.
// We use a local interface to avoid circular import engine → nodes → engine.
type RunContexter interface {
	Message() string
	UserInput() string
	ToolOutputs() map[string]any
	Set(string, any)
	Get(string) (any, bool)
	// OutputOrder returns node IDs oldest-first. Used by resolveTemplate to
	// resolve {{ node.<id> }} references deterministically.
	OutputOrder() []string
}

// OutputOrder for emptyRunContext (type defined in the frozen tendril.go) is
// added here — not in tendril.go — so the frozen file's bytes never change.
// Nil is correct: emptyRunContext has no addressable upstream outputs.
func (emptyRunContext) OutputOrder() []string { return nil }
