package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

// dryRunOutputShown bounds each step's output in a dry-run result: enough for
// the builder to read an answer or spot an empty object, not a whole page.
const dryRunOutputShown = 700

// DryRun executes a workflow graph the way a run does -- same topological
// order, same attach rules, the real executors -- except that every step
// nodes.DryRunExecutes rejects is simulated rather than performed, so nothing
// is sent, paid for, rented or written. Nothing is persisted and nothing is
// billed. The chat builder calls it to check a workflow actually produces an
// answer before it tells the user the workflow is done.
//
// input is the chat or webhook message to start from ("" for a manual run).
// platformKeys is the provider key map a platform-key agent needs.
func DryRun(ctx context.Context, graph models.WorkflowGraph, input string, platformKeys map[string]string) nodes.DryRunResult {
	res := nodes.DryRunResult{Steps: []nodes.DryRunStep{}}
	levels, err := TopologicalSort(graph.Nodes, graph.Edges)
	if err != nil {
		res.Failed, res.Error = true, err.Error()
		return res
	}
	attachMap := BuildAttachMap(graph.Nodes, graph.Edges)
	// Nodes attached to an agent are its resources, not steps -- the runner
	// skips them the same way.
	attached := map[string]bool{}
	for _, e := range graph.Edges {
		if e.Kind == models.EdgeKindAttach {
			attached[e.From] = true
		}
	}

	var inputJSON []byte
	if input != "" {
		inputJSON, _ = json.Marshal(map[string]string{"message": input})
	}
	rc := NewRunContext("dry-run", inputJSON)

	for _, level := range levels {
		for _, n := range level {
			if attached[n.ID] {
				continue
			}
			if ctx.Err() != nil {
				res.Failed, res.Error = true, "the test run ran out of time"
				return res
			}
			step := nodes.DryRunStep{NodeID: n.ID, Name: n.Name, Type: string(n.Type), Template: n.Template}
			out, reason, err := dryRunNode(ctx, n, attachMap[n.ID], rc, platformKeys)
			if err != nil {
				step.Status, step.Error = "failed", nodes.SanitizeRunError(err.Error())
				res.Steps = append(res.Steps, step)
				res.Failed = true
				return res // a run stops at its first failure, and so does this
			}
			rc.Set(n.ID, out)
			step.Output = clipOutput(out)
			switch {
			case reason != "":
				step.Status, step.Reason = "simulated", reason
			case n.Type != models.NodeTypeTrigger && n.Type != models.NodeTypeEnd && nodes.IsEmptyOutput(out):
				step.Status = "empty"
				res.Empty = true
			default:
				step.Status = "ran"
			}
			if n.Type == models.NodeTypeAgent {
				if m, ok := out.(map[string]any); ok {
					if s, ok := m["message"].(string); ok {
						res.Answer = s
					}
				}
			}
			res.Steps = append(res.Steps, step)
		}
	}
	final := rc.Message()
	res.FinalOutput = clipText(final)
	if nodes.IsEmptyOutput(final) {
		res.Empty = true
	}
	return res
}

// dryRunNode runs or simulates one node. A non-empty reason means it was
// simulated.
func dryRunNode(ctx context.Context, n models.WorkflowNode, attach models.AttachConfig, rc *RunContext, platformKeys map[string]string) (any, string, error) {
	if ok, reason := nodes.DryRunExecutes(n); !ok {
		sim := map[string]any{"simulated": true, "reason": reason}
		if n.Type == models.NodeTypeAction || n.Type == models.NodeTypeGoogle {
			sim["wouldSend"] = nodes.ResolveMessageForTest(n, rc)
		}
		return sim, reason, nil
	}
	state := rc.State()
	n.URL = nodes.ExpandState(n.URL, state)
	n.SystemPrompt = nodes.ExpandState(n.SystemPrompt, state)

	switch n.Type {
	case models.NodeTypeTrigger:
		return rc.input, "", nil
	case models.NodeTypeEnd:
		return rc.Message(), "", nil
	case models.NodeTypeProvider:
		return rc.Message(), "", nil
	case models.NodeTypeTool:
		out, err := nodes.ExecuteTool(ctx, n, rc)
		return out, "", err
	case models.NodeTypeAction:
		out, err := nodes.ExecuteAction(ctx, n, rc)
		if errors.Is(err, nodes.ErrActionSkipped) {
			return out, "", nil
		}
		return out, "", err
	case models.NodeTypeAgent:
		// Only tools a dry run may execute are handed to the agent: a paid
		// x402 tool, or an HTTP call that could change something, is left
		// out rather than invoked.
		safe := attach
		safe.Tools = nil
		for _, t := range attach.Tools {
			if ok, _ := nodes.DryRunExecutes(t); ok {
				safe.Tools = append(safe.Tools, t)
			}
		}
		out, err := nodes.ExecuteAgent(ctx, n, safe, models.AgentWallet{}, nil, rc, nil, platformKeys, nodes.X402RelayConfig{})
		return out, "", err
	}
	return nil, "", fmt.Errorf("a %s step cannot be test-run", n.Type)
}

func clipOutput(v any) string {
	if s, ok := v.(string); ok {
		return clipText(s)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return clipText(fmt.Sprint(v))
	}
	return clipText(string(b))
}

func clipText(s string) string {
	if len(s) <= dryRunOutputShown {
		return s
	}
	return s[:dryRunOutputShown] + "…"
}
