package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentmesh/backend/internal/alert"
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

const (
	byokFlatFeeUSDMicros     int64 = 10_000  // $0.01
	x402PlatformFeeUSDMicros int64 = 500_000 // $0.50
)

// preflightCheck fails a node before it runs if wf.UserID can't cover
// amountUSDMicros. Blocks outright — no soft overage — matching the
// prepaid-only model already used for credit top-ups.
func (r *Runner) preflightCheck(ctx context.Context, wf models.Workflow, amountUSDMicros int64) error {
	balance, err := r.store.GetCreditBalance(ctx, wf.UserID)
	if err != nil {
		return err
	}
	if balance < amountUSDMicros {
		return fmt.Errorf("insufficient credits: balance %d micros, need %d micros", balance, amountUSDMicros)
	}
	return nil
}

// debitOrLog charges amountUSDMicros against wf.UserID for nodeID and just
// logs on failure rather than failing the node — the node already ran
// successfully by the time this is called, so there's nothing left to roll
// back (x402 payments in particular can't be undone once sent on-chain).
func (r *Runner) debitOrLog(ctx context.Context, wf models.Workflow, run models.Run, nodeID string, amountUSDMicros int64, kind string) {
	if err := r.store.DebitCredits(ctx, wf.UserID, amountUSDMicros, kind, wf.ID, run.ID, nodeID); err != nil {
		log.Printf("debit failed: user=%s workflow=%s run=%s node=%s kind=%s amount=%d: %v",
			wf.UserID, wf.ID, run.ID, nodeID, kind, amountUSDMicros, err)
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

// finishRun records the run's terminal status and fires a workflow-run audit-log
// notification. Centralized here so every terminal path (success, failed, stopped)
// reports to the same Discord channel with the same message shape.
func (r *Runner) finishRun(wf models.Workflow, run models.Run, status models.RunStatus) {
	r.store.FinishRun(context.Background(), run.ID, status)
	go alert.Notify(context.Background(), alert.ChannelWorkflows, fmt.Sprintf("workflow %q run %s finished: %s", wf.Name, run.ID, status))
}

// Run executes a workflow. Call via Start rather than directly.
func (r *Runner) Run(ctx context.Context, wf models.Workflow, run models.Run) {
	defer r.broker.Close(run.ID)
	defer r.registry.deregister(wf.ID)

	go alert.Notify(context.Background(), alert.ChannelWorkflows, fmt.Sprintf("workflow %q run %s started", wf.Name, run.ID))

	attachMap := BuildAttachMap(wf.Nodes, wf.Edges)
	levels, err := TopologicalSort(wf.Nodes, wf.Edges)
	if err != nil {
		r.finishRun(wf, run, models.RunStatusFailed)
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
			r.finishRun(wf, run, models.RunStatusStopped)
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

				result, execErr := r.executeNode(ctx, n, attachMap, walletByAgent, rc, run, wf)
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
			r.finishRun(wf, run, models.RunStatusFailed)
			return
		}
	}

	r.finishRun(wf, run, models.RunStatusSuccess)
}

func (r *Runner) executeNode(
	ctx context.Context,
	node models.WorkflowNode,
	attachMap map[string]models.AttachConfig,
	walletByAgent map[string]models.AgentWallet,
	rc *RunContext,
	run models.Run,
	wf models.Workflow,
) (any, error) {
	switch node.Type {
	case models.NodeTypeTrigger:
		return rc.input, nil
	case models.NodeTypeEnd:
		return rc.Message(), nil
	case models.NodeTypeAgent:
		if err := r.preflightCheck(ctx, wf, byokFlatFeeUSDMicros); err != nil {
			return nil, err
		}
		aw := walletByAgent[node.ID]
		result, err := nodes.ExecuteAgent(ctx, node, attachMap[node.ID], aw, r.walletSvc, rc)
		if err != nil {
			return nil, err
		}
		r.debitOrLog(ctx, wf, run, node.ID, byokFlatFeeUSDMicros, models.DebitKindByokFlatFee)
		if m, ok := result.(map[string]any); ok {
			if payments, ok := m["x402Payments"].([]map[string]any); ok {
				for _, p := range payments {
					nodeID, _ := p["nodeId"].(string)
					r.debitOrLog(ctx, wf, run, nodeID, x402PlatformFeeUSDMicros, models.DebitKindX402PlatformFee)
				}
			}
			if nodeIDs, ok := m["billedFlatFeeNodeIds"].([]string); ok {
				for _, nodeID := range nodeIDs {
					r.debitOrLog(ctx, wf, run, nodeID, byokFlatFeeUSDMicros, models.DebitKindByokFlatFee)
				}
			}
		}
		return result, nil
	case models.NodeTypeProvider:
		return rc.Message(), nil
	case models.NodeTypeTool:
		billable := nodes.BillableFlatFee(node.Type, node.Template)
		if billable {
			if err := r.preflightCheck(ctx, wf, byokFlatFeeUSDMicros); err != nil {
				return nil, err
			}
		}
		result, err := nodes.ExecuteTool(ctx, node, rc)
		if err != nil {
			return nil, err
		}
		if billable {
			r.debitOrLog(ctx, wf, run, node.ID, byokFlatFeeUSDMicros, models.DebitKindByokFlatFee)
		}
		return result, nil
	case models.NodeTypeTool402:
		if err := r.preflightCheck(ctx, wf, x402PlatformFeeUSDMicros); err != nil {
			return nil, err
		}
		// Find the agent that has this tool attached and use its wallet.
		var aw models.AgentWallet
		for agentID, cfg := range attachMap {
			for _, t := range cfg.Tools {
				if t.ID == node.ID {
					aw = walletByAgent[agentID]
				}
			}
		}
		result, err := nodes.ExecuteTool402(ctx, node, rc, aw, r.walletSvc)
		if err != nil {
			return nil, err
		}
		if m, ok := result.(map[string]any); ok {
			if _, hasTx := m["txId"]; hasTx {
				r.debitOrLog(ctx, wf, run, node.ID, x402PlatformFeeUSDMicros, models.DebitKindX402PlatformFee)
			}
		}
		return result, nil
	case models.NodeTypeAction:
		if err := r.preflightCheck(ctx, wf, byokFlatFeeUSDMicros); err != nil {
			return nil, err
		}
		result, err := nodes.ExecuteAction(ctx, node, rc)
		if err != nil {
			return nil, err
		}
		r.debitOrLog(ctx, wf, run, node.ID, byokFlatFeeUSDMicros, models.DebitKindByokFlatFee)
		return result, nil
	default:
		return nil, nil
	}
}
