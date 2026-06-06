package engine

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
)

type Runner struct {
	store  *db.Store
	broker *sse.Broker
}

func NewRunner(store *db.Store, broker *sse.Broker) *Runner {
	return &Runner{store: store, broker: broker}
}

// Run executes a workflow asynchronously. Call as a goroutine.
func (r *Runner) Run(ctx context.Context, wf models.Workflow, run models.Run) {
	defer r.broker.Close(run.ID)

	attachMap := BuildAttachMap(wf.Nodes, wf.Edges)
	levels, err := TopologicalSort(wf.Nodes, wf.Edges)
	if err != nil {
		r.store.FinishRun(ctx, run.ID, models.RunStatusFailed)
		return
	}

	var inputJSON []byte
	if run.InputContext != nil {
		inputJSON, _ = json.Marshal(run.InputContext)
	}
	rc := NewRunContext(run.ID, inputJSON)

	var failed int32

	for stepIdx, level := range levels {
		var wg sync.WaitGroup
		for _, node := range level {
			wg.Add(1)
			go func(n models.WorkflowNode, idx int) {
				defer wg.Done()
				if atomic.LoadInt32(&failed) != 0 {
					return
				}

				start := time.Now()
				logEntry, _ := r.store.InsertRunLog(ctx, models.RunLog{
					RunID:     run.ID,
					StepIndex: idx,
					NodeID:    n.ID,
					NodeType:  n.Type,
					Status:    models.LogStatusRunning,
				})

				result, execErr := r.executeNode(ctx, n, attachMap, rc, run)
				dur := int(time.Since(start).Milliseconds())

				if execErr != nil {
					atomic.StoreInt32(&failed, 1)
					outJSON, _ := json.Marshal(execErr.Error())
					r.store.UpdateRunLog(ctx, logEntry.ID, models.LogStatusFailed, outJSON, dur)
					r.broker.Publish(run.ID, models.LogEvent{
						StepIndex:  idx,
						NodeID:     n.ID,
						NodeType:   n.Type,
						Status:     models.LogStatusFailed,
						Output:     execErr.Error(),
						DurationMs: dur,
						Ts:         time.Now(),
					})
					return
				}

				rc.Set(n.ID, result)
				outJSON, _ := json.Marshal(result)
				r.store.UpdateRunLog(ctx, logEntry.ID, models.LogStatusSuccess, outJSON, dur)
				r.broker.Publish(run.ID, models.LogEvent{
					StepIndex:  idx,
					NodeID:     n.ID,
					NodeType:   n.Type,
					Status:     models.LogStatusSuccess,
					Output:     result,
					DurationMs: dur,
					Ts:         time.Now(),
				})
			}(node, stepIdx)
		}
		wg.Wait()

		if atomic.LoadInt32(&failed) != 0 {
			r.store.FinishRun(ctx, run.ID, models.RunStatusFailed)
			return
		}
	}

	r.store.FinishRun(ctx, run.ID, models.RunStatusSuccess)
}

func (r *Runner) executeNode(ctx context.Context, node models.WorkflowNode, attachMap map[string]models.AttachConfig, rc *RunContext, run models.Run) (any, error) {
	switch node.Type {
	case models.NodeTypeTrigger:
		return rc.input, nil
	case models.NodeTypeEnd:
		return rc.Message(), nil
	case models.NodeTypeAgent:
		return nodes.ExecuteAgent(ctx, node, attachMap[node.ID], rc)
	case models.NodeTypeProvider:
		return rc.Message(), nil
	case models.NodeTypeTool:
		return nodes.ExecuteTool(ctx, node, rc)
	case models.NodeTypeTool402:
		wallet, _ := r.store.GetAgentWallet(ctx, run.WorkflowID, node.ID)
		return nodes.ExecuteTool402(ctx, node, rc, wallet, r.store)
	case models.NodeTypeAction:
		return nodes.ExecuteAction(ctx, node, rc)
	default:
		return nil, nil
	}
}
