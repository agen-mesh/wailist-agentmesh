package nodes

import (
	"context"

	"github.com/agentmesh/backend/internal/models"
)

// BillableFlatFee reports whether executing a node performs a real
// off-platform action that should be charged the flat BYOK convenience
// fee. Every Action node (email + all 22 connectors) is billable — in
// practice every Action node has a real, recognized template, since the
// UI only ever creates nodes from its connector list; the "logged"
// fallback in ExecuteAction's switch is a defensive no-op that doesn't
// occur in real workflows, so it isn't special-cased here. Agent nodes
// are always billable — every agent call is BYOK today (issue #25 will
// add a platform-key toggle, which is where this stops being true
// unconditionally). Tool nodes are billable only for the "http" template
// — "calc" and "datetime" are pure local computation, no different from
// Trigger/End, and stay free. Tool402 is metered separately (a $0.50
// flat fee gated on whether a payment actually happened at runtime, not
// on static config), so it always returns false here.
func BillableFlatFee(nodeType models.NodeType, template string) bool {
	switch nodeType {
	case models.NodeTypeAgent, models.NodeTypeAction:
		return true
	case models.NodeTypeTool:
		return template == "http"
	default:
		return false
	}
}

// BalanceChecker lets the nodes package ask the caller whether a billable
// attached tool/x402 call can proceed, without giving nodes direct DB
// access. Returns a non-nil error (matching preflightCheck's error text
// convention) when the balance is insufficient.
type BalanceChecker func(ctx context.Context, amountUSDMicros int64) error

// ErrBalanceBlocked wraps a BalanceChecker failure so the agent loop can
// hard-stop instead of feeding the failure back to the LLM as a retryable
// tool-level error (which would just spin the loop until
// maxToolIterations, contradicting the "blocks before it runs, no soft
// overage" contract). Callers use errors.As to distinguish this from other
// ExecuteAgent failures (e.g. LLM connectivity errors): a
// *ErrBalanceBlocked failure means the agent's own LLM turn already ran
// (so its flat fee is still owed) and only the subsequent attached call
// was blocked; any other error means the agent turn itself never
// completed, so nothing should be billed.
type ErrBalanceBlocked struct {
	Err error
}

func (e *ErrBalanceBlocked) Error() string { return e.Err.Error() }
func (e *ErrBalanceBlocked) Unwrap() error { return e.Err }
