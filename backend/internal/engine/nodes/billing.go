package nodes

import "github.com/agentmesh/backend/internal/models"

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
