package nodes

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/testutil"
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

func TestArcNodeCannotGrantOperatorAuthority(t *testing.T) {
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	rpc := &testutil.ArcRPC{}
	testutil.UseArcRPC(t, rpc)
	node := models.WorkflowNode{Type: models.NodeTypeArc, Template: "transfer", Price: "1", Endpoint: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
		Config: map[string]string{"broadcast": "true", "operator": "true", "userID": "owner", "worldVerified": "true"}}
	state := map[string]any{"userID": "owner", "role": "operator", "operator": true, "worldVerified": true}
	_, err := ExecuteArc(context.Background(), node, state, arc.LoadConfig())
	if !errors.Is(err, arc.ErrOperatorRequired) || rpc.Calls.Load() != 0 || IsRetryable(err) {
		t.Fatalf("forged authority: %v calls=%d", err, rpc.Calls.Load())
	}
}

func TestArcNodePendingIsNotSuccess(t *testing.T) {
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	ctx, cancel := context.WithCancel(arc.WithPrincipal(context.Background(), "owner"))
	defer cancel()
	rpc := &testutil.ArcRPC{Pending: true, OnReceipt: cancel}
	testutil.UseArcRPC(t, rpc)
	node := models.WorkflowNode{Type: models.NodeTypeArc, Template: "transfer", Price: "1", Endpoint: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC", Config: map[string]string{"broadcast": "true"}}
	out, err := ExecuteArc(ctx, node, nil, arc.LoadConfig())
	if !errors.Is(err, ErrSettlementIndeterminate) || IsRetryable(err) || rpc.Sends.Load() != 1 {
		t.Fatalf("pending: %v sends=%d", err, rpc.Sends.Load())
	}
	res := out.(arc.Result)
	if res.Status != "pending" || res.TxHash == "" || !strings.Contains(err.Error(), res.TxHash) {
		t.Fatalf("pending identity: %+v %v", res, err)
	}
}

func TestArcNodeRejectedSendIsNotIndeterminate(t *testing.T) {
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	rpc := &testutil.ArcRPC{RejectSend: "nonce too low"}
	testutil.UseArcRPC(t, rpc)
	node := models.WorkflowNode{Type: models.NodeTypeArc, Template: "transfer", Price: "1", Endpoint: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC", Config: map[string]string{"broadcast": "true"}}
	_, err := ExecuteArc(arc.WithPrincipal(context.Background(), "owner"), node, nil, arc.LoadConfig())
	if err == nil || errors.Is(err, ErrSettlementIndeterminate) || errors.Is(err, arc.ErrSubmissionUncertain) || rpc.Sends.Load() != 1 {
		t.Fatalf("rejected send: %v sends=%d", err, rpc.Sends.Load())
	}
}

func TestArcDryRunRejectsUnresolvedBroadcast(t *testing.T) {
	for _, flag := range []string{"true", "TRUE", " {{input.broadcast}} ", "{{operator}}", "1"} {
		if ok, _ := DryRunExecutes(models.WorkflowNode{Type: models.NodeTypeArc, Config: map[string]string{"broadcast": flag}}); ok {
			t.Errorf("dryrun accepted %q", flag)
		}
	}
	for _, flag := range []string{"", " false ", "FALSE"} {
		if ok, reason := DryRunExecutes(models.WorkflowNode{Type: models.NodeTypeArc, Config: map[string]string{"broadcast": flag}}); !ok {
			t.Errorf("dryrun rejected quote %q: %s", flag, reason)
		}
	}
}
