package nodes

// RunContexter interface satisfied by engine.RunContext via duck typing.
// We use a local interface to avoid circular import engine → nodes → engine.
type RunContexter interface {
	Message() string
	// LastOutput returns the most recent output's raw (unstringified) value
	// -- see expandTemplate in connector_helpers.go, which needs the real
	// structure to resolve a {{ result.field }} placeholder.
	LastOutput() any
	UserInput() string
	ToolOutputs() map[string]any
	Set(string, any)
	Get(string) (any, bool)
}
