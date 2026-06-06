package nodes

import (
	"context"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
)

// RunContexter interface satisfied by engine.RunContext via duck typing.
// We use a local interface to avoid circular import engine → nodes → engine.
type RunContexter interface {
	Message() string
	Set(string, any)
	Get(string) (any, bool)
}

func ExecuteAgent(ctx context.Context, node models.WorkflowNode, attach models.AttachConfig, rc RunContexter) (any, error) {
	return rc.Message(), nil
}
func ExecuteTool(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	return rc.Message(), nil
}
func ExecuteTool402(ctx context.Context, node models.WorkflowNode, rc RunContexter, wallet models.AgentWallet, store *db.Store) (any, error) {
	return rc.Message(), nil
}
func ExecuteAction(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	return "logged", nil
}
