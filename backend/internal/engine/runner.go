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
	store     *db.Store
	broker    *sse.Broker
	walletSvc nodes.WalletSigner
	registry  *runRegistry
}

func NewRunner(store *db.Store, broker *sse.Broker, walletSvc nodes.WalletSigner) *Runner {
	return &Runner{
		store:     store,
		broker:    broker,
		walletSvc: walletSvc,
		registry:  newRunRegistry(),
	}
}

// Start creates a cancellable context for the run, registers it, and launches
// Run in a goroutine. Replaces the previous pattern of calling Run directly.
func (r *Runner) Start(wf models.Workflow, run models.Run) {
	ctx, cancel := context.WithCancel(context.Background())
	r.registry.register(wf.ID, cancel)
	go r.Run(ctx, wf, run)
}

// Stop cancels the active run for the given workflow ID. Returns false if no
// run was registered (i.e. the workflow is not currently running).
func (r *Runner) Stop(workflowID string) bool {
	return r.registry.cancel(workflowID)
}

// Run executes a workflow. Call via Start rather than directly.
func (r *Runner) Run(ctx context.Context, wf models.Workflow, run models.Run) {
	defer r.broker.Close(run.ID)
	defer r.registry.deregister(wf.ID)

	attachMap := BuildAttachMap(wf.Nodes, wf.Edges)
	levels, err := TopologicalSort(wf.Nodes, wf.Edges)
	if err != nil {
		r.store.FinishRun(context.Background(), run.ID, models.RunStatusFailed)
		return
	}

	// Pre-load all agent wallets for this workflow so tool402 nodes can resolve
	// their parent agent's wallet without hitting the DB per-node.
	walletByAgent := make(map[string]models.AgentWallet)
	if wallets, err := r.store.ListAgentWallets(ctx, run.WorkflowID); err == nil {
		for _, w := range wallets {
			walletByAgent[w.AgentNodeID] = w
		}
	}

	var inputJSON []byte
	if run.InputContext != nil {
		inputJSON, _ = json.Marshal(run.InputContext)
	}
	rc := NewRunContext(run.ID, inputJSON)

	var failed int32

	for stepIdx, level := range levels {
		// Check for cancellation between levels.
		if ctx.Err() != nil {
			r.store.FinishRun(context.Background(), run.ID, models.RunStatusStopped)
			return
		}

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

				result, execErr := r.executeNode(ctx, n, attachMap, walletByAgent, rc, run)
				dur := int(time.Since(start).Milliseconds())

				if execErr != nil {
					atomic.StoreInt32(&failed, 1)
					outJSON, _ := json.Marshal(execErr.Error())
					r.store.UpdateRunLog(context.Background(), logEntry.ID, models.LogStatusFailed, outJSON, dur)
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
				r.store.UpdateRunLog(context.Background(), logEntry.ID, models.LogStatusSuccess, outJSON, dur)
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
			r.store.FinishRun(context.Background(), run.ID, models.RunStatusFailed)
			return
		}
	}

	r.store.FinishRun(context.Background(), run.ID, models.RunStatusSuccess)
}

func (r *Runner) executeNode(
	ctx context.Context,
	node models.WorkflowNode,
	attachMap map[string]models.AttachConfig,
	walletByAgent map[string]models.AgentWallet,
	rc *RunContext,
	run models.Run,
) (any, error) {
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
		// Find the agent that has this tool attached and use its wallet.
		var aw models.AgentWallet
		for agentID, cfg := range attachMap {
			for _, t := range cfg.Tools {
				if t.ID == node.ID {
					aw = walletByAgent[agentID]
				}
			}
		}
		return nodes.ExecuteTool402(ctx, node, rc, aw, r.walletSvc)
	case models.NodeTypeAction:
		return nodes.ExecuteAction(ctx, node, rc)
	default:
		return nil, nil
	}
}
