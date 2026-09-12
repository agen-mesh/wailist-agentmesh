package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"unicode/utf8"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

// dryRunOutputShown bounds each step's output in a dry-run result: enough for
// the builder to read an answer or spot an empty object, not a whole page.
const dryRunOutputShown = 700

// DryRunOptions is what a dry run starts from, beyond the graph itself.
type DryRunOptions struct {
	// Input is the chat or webhook message to start from ("" for a manual run).
	Input string
	// State is the workflow's stored variables, loaded as a run loads them,
	// so {{state.x}} resolves to what a real run would see.
	State map[string]any
	// PlatformKeys is the provider key map a platform-key agent needs.
	PlatformKeys map[string]string
	// CheckBalance, when set, gates each platform-key agent on its real fee
	// exactly as a run's preflight does. A test run that could call any
	// agent regardless would let a user with no credits run them for free.
	CheckBalance func(ctx context.Context, amountUSDMicros int64) error
	// ChargeAgent, when set, is called after a platform-key agent call
	// succeeds, with the same fee a run would charge. A test run costs the
	// platform exactly what a run does, so checking the balance without ever
	// debiting it would make those calls free for anyone who keeps a single
	// credit on the account. A failure to charge is logged, not surfaced:
	// the call has already happened.
	ChargeAgent func(ctx context.Context, nodeID string, amountUSDMicros int64, model string) error
}

// unverifiable is a step a dry run could not check -- not a failure of the
// workflow, so the builder is not told to fix it.
type unverifiable struct{ reason string }

func (u unverifiable) Error() string { return u.reason }

// DryRun executes a workflow graph the way a run does -- same topological
// order, same attach rules, same variables and {{state.x}} expansion, the
// real executors -- except that every step nodes.DryRunExecutes rejects is
// simulated rather than performed, so nothing is sent, paid for, rented or
// written. Nothing is persisted and nothing is billed. The chat builder calls
// it to check a workflow actually produces an answer before it tells the user
// the workflow is done.
func DryRun(ctx context.Context, graph models.WorkflowGraph, opts DryRunOptions) nodes.DryRunResult {
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
	next := map[string][]string{}
	for _, e := range graph.Edges {
		switch e.Kind {
		case models.EdgeKindAttach:
			attached[e.From] = true
		case models.EdgeKindFlow:
			next[e.From] = append(next[e.From], e.To)
		}
	}
	// fedBySimulated maps a step to the simulated step whose placeholder
	// output flows into it. What such a step does with a placeholder says
	// nothing about the workflow: a json_extract on it fails, an agent
	// handed it says there is no data. Neither is a fault to fix.
	fedBySimulated := map[string]string{}

	var inputJSON []byte
	if opts.Input != "" {
		inputJSON, _ = json.Marshal(map[string]string{"message": opts.Input})
	}
	rc := NewRunContext("dry-run", inputJSON)
	if opts.State != nil {
		rc.SetState(opts.State)
	}

	lastFedBySimulated := false
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
			source, tainted := fedBySimulated[n.ID]
			out, reason, err := dryRunNode(ctx, n, attachMap[n.ID], rc, opts)
			var u unverifiable
			switch {
			case errors.As(err, &u):
				step.Status, step.Reason = "unverified", u.reason
				res.Steps = append(res.Steps, step)
				res.Unverified = true
				return res
			case err != nil && nodes.IsMissingCredentialError(err.Error()):
				// Not a fault to fix: the builder cannot set credentials, so
				// a 401 here means the node is waiting for the user, not that
				// the workflow is wrong.
				step.Status, step.Reason = "unverified", nodes.MissingCredentialReason
				step.Error = nodes.SanitizeRunError(err.Error())
				res.Steps = append(res.Steps, step)
				res.Unverified = true
				return res
			case err != nil && tainted:
				step.Status, step.Error = "unverified", nodes.SanitizeRunError(err.Error())
				step.Reason = fmt.Sprintf("its input comes from %q, which a test run only simulates, so it cannot be checked", source)
				res.Steps = append(res.Steps, step)
				res.Unverified = true
				return res
			case err != nil:
				step.Status, step.Error = "failed", nodes.SanitizeRunError(err.Error())
				res.Steps = append(res.Steps, step)
				res.Failed = true
				return res // a run stops at its first failure, and so does this
			}
			rc.Set(n.ID, out)
			step.Output = clipOutput(out)
			isStep := n.Type != models.NodeTypeTrigger && n.Type != models.NodeTypeEnd
			switch {
			case reason != "":
				step.Status, step.Reason = "simulated", reason
				source = n.Name
				if source == "" {
					source = n.ID
				}
			case tainted && isStep:
				step.Status = "unverified"
				step.Reason = fmt.Sprintf("its input comes from %q, which a test run only simulates, so its output is not real", source)
				res.Unverified = true
			case isStep && nodes.IsEmptyOutput(out):
				step.Status = "empty"
				res.Empty = true
			default:
				step.Status = "ran"
			}
			if reason != "" || tainted {
				for _, to := range next[n.ID] {
					if _, seen := fedBySimulated[to]; !seen {
						fedBySimulated[to] = source
					}
				}
			}
			lastFedBySimulated = reason != "" || tainted
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
	if nodes.IsEmptyOutput(final) && !lastFedBySimulated {
		res.Empty = true
	}
	return res
}

// dryRunNode runs or simulates one node. A non-empty reason means it was
// simulated.
func dryRunNode(ctx context.Context, n models.WorkflowNode, attach models.AttachConfig, rc *RunContext, opts DryRunOptions) (any, string, error) {
	if ok, reason := nodes.DryRunExecutes(n); !ok {
		sim := map[string]any{"simulated": true, "reason": reason}
		if n.Type == models.NodeTypeAction || n.Type == models.NodeTypeGoogle {
			sim["wouldSend"] = nodes.ResolveMessageForTest(n, rc)
		}
		return sim, reason, nil
	}
	n = expandNodeState(n, rc.State())

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
			// A connector skips itself when its credential is missing. In a
			// run that is a step doing nothing; in a test it means this step
			// was never checked, which the builder must not read as success.
			return out, "", unverifiable{nodes.MissingCredentialReason}
		}
		return out, "", err
	case models.NodeTypeAgent:
		// The same preflight a run applies (Runner.executeNode): a
		// platform-key agent runs only if the user could pay for it -- and,
		// below, it is charged for it.
		var platformFee int64
		var platformModel string
		if p := attach.Provider; p != nil && p.KeyMode == "platform" {
			platformModel = nodes.ResolveModel(p.Template, p.Model)
			platformFee = nodes.PlatformKeyFeeUSDMicros(nodes.ModelTier(p.Template, platformModel))
			if opts.CheckBalance != nil {
				if err := opts.CheckBalance(ctx, platformFee); err != nil {
					return nil, "", unverifiable{"this agent runs on AgentMesh credits and the account does not have enough credits to test it"}
				}
			}
		}
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
		out, err := nodes.ExecuteAgent(ctx, n, safe, models.AgentWallet{}, nil, rc, nil, opts.PlatformKeys, nodes.X402RelayConfig{})
		// Charged after the call, like a run (Runner.debitOrLog): the model
		// has already been paid for by then, so a failed charge is logged
		// rather than turned into a step failure.
		if err == nil && platformFee > 0 && opts.ChargeAgent != nil {
			if cerr := opts.ChargeAgent(ctx, n.ID, platformFee, platformModel); cerr != nil {
				log.Printf("dry run: charge agent %s (%d micros): %v", n.ID, platformFee, cerr)
			}
		}
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

// clipText bounds a value at dryRunOutputShown bytes without splitting a
// rune: the result is marshalled into the payload sent to the model and
// shown in chat, and half a rune is invalid UTF-8 in both.
func clipText(s string) string {
	if len(s) <= dryRunOutputShown {
		return s
	}
	cut := dryRunOutputShown
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
