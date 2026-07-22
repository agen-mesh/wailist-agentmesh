package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
)

// TestMain sets the permissive URL validator once for the whole engine_test
// binary, mirroring the identical override in internal/engine/nodes'
// TestMain (tool402_test.go). Without it, the SSRF guard in nodes.ExecuteTool
// blocks every httptest.NewServer target (127.0.0.1) with "requests to
// private/internal addresses are not allowed" — unrelated to billing, but it
// otherwise prevents these tests from ever observing a successful HTTP call.
// No test in this package exercises the real SSRF-blocking validator.
func TestMain(m *testing.M) {
	nodes.SetURLValidatorForTest(func(string) error { return nil })
	os.Exit(m.Run())
}

// fundUser mirrors the identical helper in internal/db/debit_test.go — kept
// separate since it's a different package and this is the only place it's
// needed here.
func fundUser(t *testing.T, store *db.Store, userID string, micros int64) {
	t.Helper()
	ctx := context.Background()
	orderID := fmt.Sprintf("fund_%s_%d", userID, time.Now().UnixNano())
	fxRate := float64(micros) / 1e6
	if _, err := store.CreateCreditTransaction(ctx, userID, orderID, 100, fxRate); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompleteCreditTransaction(ctx, orderID, "pay_"+orderID); err != nil {
		t.Fatal(err)
	}
}

func waitForRunDone(t *testing.T, store *db.Store, runID string) models.Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.GetRun(context.Background(), runID)
		if err == nil && run.Status != models.RunStatusRunning {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run did not finish in time")
	return models.Run{}
}

func TestByokFlatFeeChargedOnHTTPToolSuccess(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	email := fmt.Sprintf("byok-tool-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, 100000) // 10 cents

	wf, err := store.CreateWorkflow(ctx, "BYOK Tool Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeTool, Template: "http", URL: srv.URL, Method: "GET"},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusSuccess {
		t.Fatalf("want success got %s", final.Status)
	}
	if hits != 1 {
		t.Fatalf("want exactly 1 request to the test server, got %d", hits)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 90000 {
		t.Fatalf("want balance 90000 got %d", balance)
	}

	entries, err := store.ListDebitLedger(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != models.DebitKindByokFlatFee || entries[0].AmountUSDMicros != 10000 {
		t.Fatalf("unexpected ledger entries: %+v", entries)
	}
}

func TestToolCalcNodeNotCharged(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	email := fmt.Sprintf("free-calc-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, 10000) // exactly one flat fee's worth

	wf, err := store.CreateWorkflow(ctx, "Free Calc Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeTool, Template: "calc", URL: "1+1"},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusSuccess {
		t.Fatalf("want success got %s", final.Status)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 10000 {
		t.Fatalf("calc node must stay free: want balance unchanged at 10000, got %d", balance)
	}
}

func TestInsufficientBalanceBlocksToolNodeBeforeExecution(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	email := fmt.Sprintf("broke-tool-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	// No funding — balance starts at 0, below the 10000-micros flat fee.

	wf, err := store.CreateWorkflow(ctx, "Broke Tool Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeTool, Template: "http", URL: srv.URL, Method: "GET"},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "n2", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusFailed {
		t.Fatalf("want failed got %s", final.Status)
	}
	if hits != 0 {
		t.Fatalf("want zero requests to the test server (blocked pre-flight), got %d", hits)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance unchanged at 0, got %d", balance)
	}
}

func TestAgentNodeChargesOwnFeeAndAttachedToolCalls(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	toolSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":"tool ran"}`))
	}))
	defer toolSrv.Close()

	callCount := 0
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_tool","arguments":"{}"}}]}}]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`))
	}))
	defer llmSrv.Close()
	nodes.SetOpenAIBaseURL(llmSrv.URL)
	defer nodes.SetOpenAIBaseURL("https://api.openai.com")

	email := fmt.Sprintf("agent-fee-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, 100000) // 10 cents: 1 agent fee + 1 tool fee = 20000, plenty of headroom

	wf, err := store.CreateWorkflow(ctx, "Agent Fee Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "agent1", Type: models.NodeTypeAgent},
			{ID: "provider1", Type: models.NodeTypeProvider, Template: "openai", APIKey: "test-key", Model: "gpt-4o"},
			{ID: "tool1", Type: models.NodeTypeTool, Name: "search_tool", Template: "http", URL: toolSrv.URL, Method: "GET"},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "agent1", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "agent1", To: "n3", Kind: models.EdgeKindFlow},
			{ID: "e3", From: "provider1", To: "agent1", Kind: models.EdgeKindAttach, ToPort: "model"},
			{ID: "e4", From: "tool1", To: "agent1", Kind: models.EdgeKindAttach, ToPort: "tools"},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte(`{"message":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusSuccess {
		t.Fatalf("want success got %s", final.Status)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 100000 - 10000 (agent's own fee) - 10000 (attached tool call) = 80000
	if balance != 80000 {
		t.Fatalf("want balance 80000 got %d", balance)
	}

	entries, err := store.ListDebitLedger(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 ledger entries (agent fee + tool fee), got %d: %+v", len(entries), entries)
	}
	var sawAgentFee, sawToolFee bool
	for _, e := range entries {
		if e.NodeID == "agent1" && e.Kind == models.DebitKindByokFlatFee {
			sawAgentFee = true
		}
		if e.NodeID == "tool1" && e.Kind == models.DebitKindByokFlatFee {
			sawToolFee = true
		}
	}
	if !sawAgentFee || !sawToolFee {
		t.Fatalf("want one ledger entry for agent1 and one for tool1, got %+v", entries)
	}
}

func TestStandaloneTool402ChargesFeeOnlyWhenPaymentSent(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Payment-Txid") != "" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	email := fmt.Sprintf("x402-standalone-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, 1000000) // $1, plenty for one $0.50 fee

	wf, err := store.CreateWorkflow(ctx, "X402 Standalone Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	// Deliberately no agent wallet and no attach edge for x1 — this exercises
	// the "no signer configured" path (see the note after this test).

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "x1", Type: models.NodeTypeTool402, Endpoint: srv.URL},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "x1", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "x1", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusSuccess {
		t.Fatalf("want success got %s", final.Status)
	}

	// No agent attach edge targets x1, so runner.executeNode resolves an empty
	// AgentWallet for it — ExecuteTool402 degrades gracefully (no signer
	// configured), so no payment is sent and no fee should be charged.
	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 1000000 {
		t.Fatalf("want balance unchanged at 1000000 (no wallet configured, no payment sent), got %d", balance)
	}
	entries, err := store.ListDebitLedger(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 ledger entries, got %d", len(entries))
	}
}

func TestAgentBlocksAttachedX402CallWhenBalanceInsufficientForFee(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	var x402Hits int
	x402Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x402Hits++
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer x402Srv.Close()

	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{{
					"id":       "call_1",
					"type":     "function",
					"function": map[string]any{"name": "paid_tool", "arguments": "{}"},
				}},
			}}},
		})
	}))
	defer llmSrv.Close()

	email := fmt.Sprintf("agent-x402-broke-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	// Exactly enough for the agent's own $0.01 fee, nowhere near the attached
	// tool402 call's $0.50 fee.
	fundUser(t, store, user.ID, 10_000)

	wf, err := store.CreateWorkflow(ctx, "Agent X402 Broke Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	nodes.SetOpenAIBaseURL(llmSrv.URL)

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "p1", Type: models.NodeTypeProvider, Template: "openai", APIKey: "test-key", Model: "gpt-4o"},
			{ID: "a1", Type: models.NodeTypeAgent},
			{ID: "x1", Type: models.NodeTypeTool402, Name: "paid_tool", Endpoint: x402Srv.URL},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "a1", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "a1", To: "n3", Kind: models.EdgeKindFlow},
			{ID: "e3", From: "p1", To: "a1", Kind: models.EdgeKindAttach, ToPort: "model"},
			{ID: "e4", From: "x1", To: "a1", Kind: models.EdgeKindAttach, ToPort: "tools"},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusFailed {
		t.Fatalf("want failed got %s", final.Status)
	}
	if x402Hits != 0 {
		t.Fatalf("want zero requests to the x402 server (blocked before execution), got %d", x402Hits)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance 0 (agent's own $0.01 fee charged, attached call blocked before it could spend anything else), got %d", balance)
	}
	entries, err := store.ListDebitLedger(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 ledger entry (the agent's own fee), got %d", len(entries))
	}
	if entries[0].Kind != models.DebitKindByokFlatFee || entries[0].NodeID != "a1" {
		t.Fatalf("want a single byok_flat_fee entry for node a1, got kind=%s node=%s", entries[0].Kind, entries[0].NodeID)
	}
}

func TestActionSkipPathNotBilled(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	email := fmt.Sprintf("action-skip-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, 10_000) // exactly $0.01, would cover the fee if charged

	wf, err := store.CreateWorkflow(ctx, "Action Skip Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			// Slack action node with no webhook URL configured — skip path.
			{ID: "a1", Type: models.NodeTypeAction, Template: "slack"},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "a1", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "a1", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusSuccess {
		t.Fatalf("want success got %s", final.Status)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 10_000 {
		t.Fatalf("want balance unchanged at 10000 (skipped action, no billable work), got %d", balance)
	}
	entries, err := store.ListDebitLedger(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 ledger entries (skipped action not billed), got %d", len(entries))
	}
}

func TestPlatformKeyAgentRunDebitsTierFeeAndRecordsUsage(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer platform-secret" {
			t.Errorf("want platform key, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "hi"}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer llmSrv.Close()
	nodes.SetOpenAIBaseURL(llmSrv.URL)
	defer nodes.SetOpenAIBaseURL("https://api.openai.com")

	runner.SetPlatformKeys(map[string]string{"openai": "platform-secret"})

	email := fmt.Sprintf("platform-key-agent-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, 100000) // 10 cents

	wf, err := store.CreateWorkflow(ctx, "Platform Key Agent Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "agent1", Type: models.NodeTypeAgent},
			{ID: "provider1", Type: models.NodeTypeProvider, Template: "openai", KeyMode: "platform", Model: "gpt-4.1"},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "agent1", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "agent1", To: "n3", Kind: models.EdgeKindFlow},
			{ID: "e3", From: "provider1", To: "agent1", Kind: models.EdgeKindAttach, ToPort: "model"},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte(`{"message":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusSuccess {
		t.Fatalf("want success got %s", final.Status)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 70000 { // 100000 - 30000 (gpt-4.1 is "standard" tier, $0.03)
		t.Fatalf("balance = %d, want 70000", balance)
	}

	entries, err := store.ListDebitLedger(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d debit entries, want 1: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Kind != models.DebitKindPlatformKeyLLMFee {
		t.Fatalf("kind = %q, want %q", e.Kind, models.DebitKindPlatformKeyLLMFee)
	}
	if e.AmountUSDMicros != 30000 {
		t.Fatalf("amount = %d, want 30000", e.AmountUSDMicros)
	}
	if e.TokensIn == nil || *e.TokensIn != 10 || e.TokensOut == nil || *e.TokensOut != 5 {
		t.Fatalf("usage = tokensIn=%v tokensOut=%v, want 10/5", e.TokensIn, e.TokensOut)
	}
}

func TestInsufficientBalanceBlocksTool402BeforeExecution(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("X-Payment-Required", `{"price":"0.001","unit":"call","network":"algorand-testnet","recipient":"ALGO123"}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	email := fmt.Sprintf("x402-broke-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, 100000) // 10 cents, below the $0.50 fee

	wf, err := store.CreateWorkflow(ctx, "X402 Broke Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "x1", Type: models.NodeTypeTool402, Endpoint: srv.URL},
			{ID: "n3", Type: models.NodeTypeEnd},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "n1", To: "x1", Kind: models.EdgeKindFlow},
			{ID: "e2", From: "x1", To: "n3", Kind: models.EdgeKindFlow},
		},
	}
	wf, _ = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	broker := sse.NewBroker()
	broker.Create(run.ID)

	runner.Start(wf, run)
	final := waitForRunDone(t, store, run.ID)
	if final.Status != models.RunStatusFailed {
		t.Fatalf("want failed got %s", final.Status)
	}
	if hits != 0 {
		t.Fatalf("want zero requests to the test server (blocked pre-flight), got %d", hits)
	}
	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 100000 {
		t.Fatalf("want balance unchanged at 100000, got %d", balance)
	}
}
