package nodes

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/models"
)

func arcTestConfig() arc.Config {
	return arc.Config{Network: arc.Testnet, MaxMicros: big.NewInt(1_000_000_000)}
}

func TestExecuteArcNanopayQuotesWithoutBroadcast(t *testing.T) {
	out, err := ExecuteArc(context.Background(), models.WorkflowNode{
		Type:     models.NodeTypeArc,
		Template: "nanopay",
		Price:    "0.01",
		Endpoint: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
		Config:   map[string]string{"chain": "arc-testnet", "broadcast": "false"},
	}, nil, arcTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	res := out.(arc.Result)
	if res.Status != "quoted" || res.Micros != "10000" {
		t.Fatalf("quote = %+v", res)
	}
	if res.TxHash != "" {
		t.Fatalf("quote broadcast a transaction: %s", res.TxHash)
	}
}

func TestExecuteArcRequiresRecipient(t *testing.T) {
	_, err := ExecuteArc(context.Background(), models.WorkflowNode{
		Type:     models.NodeTypeArc,
		Template: "transfer",
		Price:    "1",
		Config:   map[string]string{"chain": "arc-testnet"},
	}, nil, arcTestConfig())
	if err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteArcUnknownTemplate(t *testing.T) {
	_, err := ExecuteArc(context.Background(), models.WorkflowNode{
		Type:     models.NodeTypeArc,
		Template: "wire",
		Price:    "1",
		Endpoint: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
	}, nil, arcTestConfig())
	if err == nil || !strings.Contains(err.Error(), "unknown template") {
		t.Fatalf("err = %v", err)
	}
}

func TestDryRunRefusesArcBroadcast(t *testing.T) {
	ok, reason := DryRunExecutes(models.WorkflowNode{
		Type:   models.NodeTypeArc,
		Config: map[string]string{"broadcast": "true"},
	})
	if ok || !strings.Contains(reason, "broadcast") {
		t.Fatalf("broadcast dry run = %v %q", ok, reason)
	}
	ok, reason = DryRunExecutes(models.WorkflowNode{Type: models.NodeTypeArc, Template: "nanopay"})
	if !ok || reason != "" {
		t.Fatalf("quote dry run = %v %q", ok, reason)
	}
}
