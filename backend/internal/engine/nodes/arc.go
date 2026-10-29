package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/purchase"
)

// ExecuteArc quotes or broadcasts one Arc treasury step.
//
// Templates: "nanopay", "transfer", "bridge", "gateway". An empty template
// is a nanopay. Amount falls back to the node's price. The recipient falls
// back to the node's endpoint. "chain", "destChain", and "broadcast" live
// in config. Broadcast stays off unless config "broadcast" is the string
// true. Settlement is USDC on the chain. AgentMesh credits are not debited.
// The default chain is Arc testnet when ARC_NETWORK is not mainnet.
func ExecuteArc(ctx context.Context, node models.WorkflowNode, state map[string]any, cfg arc.Config) (any, error) {
	template := strings.TrimSpace(node.Template)
	if template == "" {
		template = "nanopay"
	}
	switch template {
	case "nanopay", "transfer", "bridge", "gateway":
	default:
		return nil, fmt.Errorf("arc: unknown template %q", node.Template)
	}
	chain := cfgString(node, "chain", state)
	destChain := cfgString(node, "destChain", state)
	broadcast := strings.EqualFold(cfgString(node, "broadcast", state), "true")
	amount := strings.TrimSpace(node.Price)
	if v := cfgString(node, "amount", state); v != "" {
		amount = v
	}
	amount = ExpandState(amount, state)
	recipient := ExpandState(strings.TrimSpace(node.Endpoint), state)
	res, err := arc.Execute(ctx, arc.HTTPCaller{}, cfg, arc.Request{
		Op:          template,
		ChainID:     chain,
		DestChainID: destChain,
		To:          recipient,
		Amount:      amount,
		Broadcast:   broadcast,
	})
	if errors.Is(err, arc.ErrSubmissionUncertain) {
		return res, fmt.Errorf("%w: tx=%s approval=%s: %w", ErrSettlementIndeterminate, res.TxHash, res.ApproveTx, err)
	}
	if err != nil {
		return nil, err
	}
	if broadcast && res.Status != "success" {
		return res, fmt.Errorf("%w: Arc broadcast status %q, tx=%s approval=%s requires reconciliation", ErrSettlementIndeterminate, res.Status, res.TxHash, res.ApproveTx)
	}
	return res, nil
}

func PurchaseRequest(node models.WorkflowNode, state map[string]any, message string) (purchase.CreateRequest, error) {
	body := ExpandState(node.Config["request"], state)
	if body == "" {
		raw, err := json.Marshal(map[string]string{"text": message})
		if err != nil {
			return purchase.CreateRequest{}, err
		}
		body = string(raw)
	}
	return purchase.CreateRequest{OfferKey: node.Config["offerKey"], MaxMicros: node.Config["maxMicros"], Request: json.RawMessage(body)}, nil
}

// Interpolated flags can change with workflow state; only literals prove a quote.
func ArcMayBroadcast(node models.WorkflowNode) bool {
	flag := strings.TrimSpace(node.Config["broadcast"])
	return flag != "" && !strings.EqualFold(flag, "false")
}

func cfgString(node models.WorkflowNode, key string, state map[string]any) string {
	if node.Config == nil {
		return ""
	}
	return ExpandState(strings.TrimSpace(node.Config[key]), state)
}
