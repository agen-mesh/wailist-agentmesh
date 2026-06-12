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
		r.store.FinishRunWithCost(context.Background(), run.ID, models.RunStatusFailed, 0)
		return
	}

	// Build set of tool/tool402 nodes that are ONLY connected via attach edges to
	// agents. These must NOT be executed as standalone topology steps — the agent
	// LLM drives them through function calling at runtime.
	agentToolIDs := make(map[string]bool)
	for _, e := range wf.Edges {
		if e.Kind == models.EdgeKindAttach && e.ToPort == "tools" {
			agentToolIDs[e.From] = true
		}
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
			r.store.FinishRunWithCost(context.Background(), run.ID, models.RunStatusStopped, 0)
			return
		}

		var wg sync.WaitGroup
		for _, node := range level {
			wg.Add(1)
			go func(n models.WorkflowNode, idx int) {
				defer wg.Done()
				// Skip attached tools — the agent invokes them via function calling.
				if agentToolIDs[n.ID] {
					return
				}
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
				// Publish a separate log event per x402 payment made inside the agent loop.
				if m, ok := result.(map[string]any); ok {
					if payments, ok := m["x402Payments"].([]map[string]any); ok {
						for _, p := range payments {
							nodeID, _ := p["nodeId"].(string)
							r.broker.Publish(run.ID, models.LogEvent{
								StepIndex:  idx,
								NodeID:     nodeID,
								NodeType:   models.NodeTypeTool402,
								Status:     models.LogStatusSuccess,
								Output:     p,
								DurationMs: 0,
								Ts:         time.Now(),
							})
						}
					}
				}
			}(node, stepIdx)
		}
		wg.Wait()

		if atomic.LoadInt32(&failed) != 0 {
			cost := r.calcRunCost(wf.Nodes)
			r.store.FinishRunWithCost(context.Background(), run.ID, models.RunStatusFailed, cost)
			r.deductUserCredits(context.Background(), wf.UserID, cost)
			return
		}
	}

	cost := r.calcRunCost(wf.Nodes)
	r.store.FinishRunWithCost(context.Background(), run.ID, models.RunStatusSuccess, cost)
	r.deductUserCredits(context.Background(), wf.UserID, cost)
}

// calcRunCost estimates the cost of a run based on the workflow nodes.
// Provider cost depends on template; tool402 nodes add a fixed per-call fee.
func (r *Runner) calcRunCost(nodes []models.WorkflowNode) float64 {
	var total float64
	for _, n := range nodes {
		switch n.Type {
		case models.NodeTypeProvider:
			base := 0.001
			switch n.Template {
			case "openai":
				base = 0.0015
			case "anthropic":
				base = 0.002
			case "gemini":
				base = 0.0008
			}
			if n.UseOurKey {
				base *= 1.3
			}
			total += base * 3 // default 3 iterations
		case models.NodeTypeTool402:
			total += 0.002
		}
	}
	return total
}

func (r *Runner) deductUserCredits(ctx context.Context, userID string, amount float64) {
	if userID == "" || amount <= 0 {
		return
	}
	r.store.DeductCredits(ctx, userID, amount)
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
		aw := walletByAgent[node.ID]
		return nodes.ExecuteAgent(ctx, node, attachMap[node.ID], aw, r.walletSvc, rc)
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
