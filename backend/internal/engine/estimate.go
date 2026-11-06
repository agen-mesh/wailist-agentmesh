package engine

import (
	"strconv"
	"strings"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

// typicalAgentToolCalls is the high-end multiplier for an agent-driven x402
// tool in the pre-run estimate: how many times we assume one agent turn
// pays that tool. Deliberately far below the real hard cap
// (nodes.maxToolIterations, 15) -- a real agent turn almost never spends 15
// paid calls, and a 15x band is too wide to be a useful "what will this
// cost" hint. It is not a billing control; drift here only loosens the UI
// estimate, never a charge.
const typicalAgentToolCalls int64 = 3

// x402EstimateCeilingUSDMicros bounds the high end of an x402 call whose
// price the graph does not state (the node carries no discovered price).
// Deliberately much smaller than models.MaxSingleX402QuoteUSDMicros -- that
// constant guards a running sum against integer overflow from an
// adversarial quote; this one is just "a typical paid endpoint costs at
// most about this" for a UI hint.
const x402EstimateCeilingUSDMicros int64 = 500_000 // $0.50

// CostEstimate is a static, pre-run guess at what one execution of a
// workflow will debit, as a low/high band in USD micros. It is derived
// only from the workflow graph -- no network probes, no DB reads -- so the
// canvas can recompute it cheaply on every deploy.
type CostEstimate struct {
	LowUSDMicros    int64              `json:"lowUsdMicros"`
	HighUSDMicros   int64              `json:"highUsdMicros"`
	Lines           []CostEstimateLine `json:"lines"`
	HasUnpricedX402 bool               `json:"hasUnpricedX402"`
}

// CostEstimateLine is one billable node's contribution to the band.
type CostEstimateLine struct {
	NodeID        string `json:"nodeId"`
	Label         string `json:"label"`
	LowUSDMicros  int64  `json:"lowUsdMicros"`
	HighUSDMicros int64  `json:"highUsdMicros"`
	Note          string `json:"note,omitempty"`
}

// EstimateRunCost walks the graph and sums each billable node's low/high
// contribution. Flat-fee nodes (agent turns, connectors, http/websearch
// tools) have low == high. The spread comes from x402 payments: an
// agent-driven paid tool can fire once (low) or up to maxAgentToolCalls
// times (high), and an endpoint with no discovered price contributes only
// the platform fee at the low end and a ceiling guess at the high end.
func EstimateRunCost(wf models.Workflow) CostEstimate {
	attach := BuildAttachMap(wf.Nodes, wf.Edges)

	// Tool / tool402 nodes wired into an agent's "tools" port are driven by
	// the LLM loop rather than the topology, so they are costed under their
	// agent, not as standalone nodes.
	attachedToolIDs := make(map[string]bool)
	for _, cfg := range attach {
		for _, t := range cfg.Tools {
			attachedToolIDs[t.ID] = true
		}
	}

	var est CostEstimate
	add := func(line CostEstimateLine) {
		est.Lines = append(est.Lines, line)
		est.LowUSDMicros += line.LowUSDMicros
		est.HighUSDMicros += line.HighUSDMicros
	}

	for _, n := range wf.Nodes {
		switch n.Type {
		case models.NodeTypeAgent:
			fee := models.ByokFlatFeeUSDMicros
			note := "agent turn (BYOK flat fee)"
			if n.KeyMode == "platform" {
				tier := nodes.ModelTier(n.Template, n.Model)
				fee = nodes.PlatformKeyFeeUSDMicros(tier)
				note = "agent turn (platform key, " + tier + ")"
			}
			low, high := fee, fee
			if cfg, ok := attach[n.ID]; ok {
				for _, t := range cfg.Tools {
					if t.Type != models.NodeTypeTool402 {
						continue
					}
					callLow, callHigh, priced := x402CallBand(t)
					low += callLow
					high += callHigh * typicalAgentToolCalls
					if !priced {
						est.HasUnpricedX402 = true
					}
				}
			}
			add(CostEstimateLine{n.ID, estimateNodeLabel(n), low, high, note})

		case models.NodeTypeAction, models.NodeTypeGoogle:
			add(CostEstimateLine{
				n.ID, estimateNodeLabel(n),
				models.ByokFlatFeeUSDMicros, models.ByokFlatFeeUSDMicros,
				"connector call (flat fee)",
			})

		case models.NodeTypeTool:
			if n.Template == "http" || n.Template == "websearch" {
				add(CostEstimateLine{
					n.ID, estimateNodeLabel(n),
					models.ByokFlatFeeUSDMicros, models.ByokFlatFeeUSDMicros,
					n.Template + " tool (flat fee)",
				})
			}
			// calc / datetime are pure local computation: free, no line.

		case models.NodeTypeArc:
			add(CostEstimateLine{
				n.ID, estimateNodeLabel(n),
				0, 0,
				"USDC settles on the chain. AgentMesh credits are not debited.",
			})

		case models.NodeTypeTool402:
			if attachedToolIDs[n.ID] {
				continue // costed under its agent above
			}
			low, high, priced := x402CallBand(n)
			note := "x402 payment + platform fee"
			if !priced {
				est.HasUnpricedX402 = true
				note = "x402 payment (price set at run time) + platform fee"
			}
			add(CostEstimateLine{n.ID, estimateNodeLabel(n), low, high, note})
		}
	}
	return est
}

// x402CallBand returns the low/high USD-micros cost of a single call to one
// x402 node, and whether the node carried a usable discovered price. When
// it did, low == high == platform fee + that price. When it did not, the
// low end is just the platform fee and the high end adds a ceiling guess.
func x402CallBand(n models.WorkflowNode) (low, high int64, priced bool) {
	fee := models.X402PlatformFeeUSDMicros
	if px, ok := parsePriceUSDMicros(n.Price); ok && px > 0 {
		return fee + px, fee + px, true
	}
	return fee, fee + x402EstimateCeilingUSDMicros, false
}

// parsePriceUSDMicros reads a node's Price field -- a plain decimal-dollar
// string the frontend stores from x402 discovery, e.g. "0.01" or "$0.20" --
// into USD micros. Returns ok=false for an empty, non-numeric, or negative
// value so the caller falls back to the unpriced band.
func parsePriceUSDMicros(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return int64(f * 1_000_000), true
}

func estimateNodeLabel(n models.WorkflowNode) string {
	switch {
	case n.Name != "":
		return n.Name
	case n.Label != "":
		return n.Label
	default:
		return string(n.Type)
	}
}
