package nodes

// RunContexter interface satisfied by engine.RunContext via duck typing.
// We use a local interface to avoid circular import engine → nodes → engine.
type RunContexter interface {
	Message() string
	UserInput() string
	ToolOutputs() map[string]any
	Set(string, any)
	Get(string) (any, bool)
}
