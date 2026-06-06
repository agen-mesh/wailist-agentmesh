package nodes

import (
	"context"

	"github.com/agentmesh/backend/internal/models"
)

// RunContexter interface satisfied by engine.RunContext via duck typing.
// We use a local interface to avoid circular import engine → nodes → engine.
type RunContexter interface {
	Message() string
	Set(string, any)
	Get(string) (any, bool)
}

func ExecuteAction(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	return "logged", nil
}
