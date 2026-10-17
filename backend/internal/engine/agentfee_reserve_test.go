package engine_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
)

// TestPlatformAgentFeeReservedBeforeAttachedCalls is the regression test for
// #29: a platform-key agent's own fee used to be checked with a plain read
// before its turn and only debited after it. An attached billable call made
// during the turn could therefore spend the balance the agent's fee needed,
// and the late debit would fail and be dropped, leaving the turn unbilled.
//
// Balance = one attached-tool fee exactly. The agent's fee is now reserved
// before the turn starts, so the attached call no longer fits: it is blocked
// before its request goes out, the run fails, and the agent's fee (its turn
// did run) is the only charge.
func TestPlatformAgentFeeReservedBeforeAttachedCalls(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	var toolHits int32
	toolSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&toolHits, 1)
		w.Write([]byte(`{"result":"tool ran"}`))
	}))
	defer toolSrv.Close()

	var llmCalls int32
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&llmCalls, 1) == 1 {
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_tool","arguments":"{}"}}]}}]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`))
	}))
	defer llmSrv.Close()
	nodes.SetOpenAIBaseURL(llmSrv.URL)
	defer nodes.SetOpenAIBaseURL("https://api.openai.com")
	runner.SetPlatformKeys(map[string]string{"openai": "platform-secret"})

	email := fmt.Sprintf("agent-fee-reserve-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	// Covers the attached tool's fee alone, or the agent's fee alone, never both.
	fundUser(t, store, user.ID, models.ByokFlatFeeUSDMicros)

	wf, err := store.CreateWorkflow(ctx, "Agent Fee Reserve Test", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "agent1", Type: models.NodeTypeAgent},
			{ID: "provider1", Type: models.NodeTypeProvider, Template: "openai", KeyMode: "platform", Model: "gpt-4.1"},
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
	if final.Status != models.RunStatusFailed {
		t.Fatalf("want failed (attached call blocked by the reserved agent fee), got %s", final.Status)
	}

	if got := atomic.LoadInt32(&toolHits); got != 0 {
		t.Fatalf("want the attached tool never called once the agent fee is held, got %d requests", got)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	// gpt-4.1 is "standard" tier.
	wantBalance := models.ByokFlatFeeUSDMicros - models.PlatformKeyStandardFeeUSDMicros
	if balance != wantBalance {
		t.Fatalf("want balance %d (agent fee charged, tool never ran), got %d", wantBalance, balance)
	}

	entries, err := store.ListDebitLedger(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 ledger entry (the agent's own fee), got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.NodeID != "agent1" || e.Kind != models.DebitKindPlatformKeyLLMFee || e.AmountUSDMicros != models.PlatformKeyStandardFeeUSDMicros {
		t.Fatalf("want one %s entry of %d for agent1, got %+v", models.DebitKindPlatformKeyLLMFee, models.PlatformKeyStandardFeeUSDMicros, e)
	}
}

// TestPlatformAgentFeeReleasedWhenTurnNeverRuns covers the release half of
// reserving up front: when the LLM call itself fails, no turn happened and
// nothing is owed, so the held fee must go back and no ledger row is written.
func TestPlatformAgentFeeReleasedWhenTurnNeverRuns(t *testing.T) {
	runner, store := newTestRunner(t)
	ctx := context.Background()

	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer llmSrv.Close()
	nodes.SetOpenAIBaseURL(llmSrv.URL)
	defer nodes.SetOpenAIBaseURL("https://api.openai.com")
	runner.SetPlatformKeys(map[string]string{"openai": "platform-secret"})

	email := fmt.Sprintf("agent-fee-release-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, models.PlatformKeyStandardFeeUSDMicros)

	wf, err := store.CreateWorkflow(ctx, "Agent Fee Release Test", user.ID)
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
	if final.Status != models.RunStatusFailed {
		t.Fatalf("want failed (LLM call rejected), got %s", final.Status)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != models.PlatformKeyStandardFeeUSDMicros {
		t.Fatalf("want balance restored to %d, got %d", models.PlatformKeyStandardFeeUSDMicros, balance)
	}

	entries, err := store.ListDebitLedger(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 ledger entries when the turn never ran, got %+v", entries)
	}
}

func TestPlatformAgentFeeCurrentSettings(t *testing.T) {
	for _, mode := range []string{"over_limit", "exact_limit", "missing", "disabled", "other_owner", "insufficient", "byok", "prefund_rejected"} {
		t.Run(mode, func(t *testing.T) {
			runner, store := newTestRunner(t)
			ctx := t.Context()
			fee := models.PlatformKeyStandardFeeUSDMicros
			balance := fee + 1_000_000
			if mode == "insufficient" {
				balance = fee - 1
			}
			if mode == "byok" {
				balance = 0
			}
			user, err := store.CreateUser(ctx, fmt.Sprintf("agent-settings-%s-%d@example.test", mode, time.Now().UnixNano()), "hash")
			if err != nil {
				t.Fatal(err)
			}
			if balance > 0 {
				fundUser(t, store, user.ID, balance)
			}
			limit := fee
			settingsOwner := user.ID
			if mode == "over_limit" || mode == "byok" || mode == "other_owner" {
				limit = models.X402ProbeFloorUSDMicros
			}
			if mode == "other_owner" {
				other, err := store.CreateUser(ctx, fmt.Sprintf("other-agent-settings-%d@example.test", time.Now().UnixNano()), "hash")
				if err != nil {
					t.Fatal(err)
				}
				settingsOwner = other.ID
			}
			if mode != "missing" {
				patch := models.UserSettingsPatch{SetMaxCallSpend: true, MaxCallSpendUSDMicros: &limit}
				if mode == "disabled" {
					patch.MaxCallSpendUSDMicros = nil
				}
				if _, err := store.UpsertUserSettings(ctx, settingsOwner, patch); err != nil {
					t.Fatal(err)
				}
			}
			var turns, quotes, payments atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				turns.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}],"usage":{"prompt_tokens":7,"completion_tokens":11}}`))
			}))
			defer provider.Close()
			nodes.SetOpenAIBaseURL(provider.URL)
			defer nodes.SetOpenAIBaseURL("https://api.openai.com")
			runner.SetPlatformKeys(map[string]string{"openai": "platform-secret"})
			wf, err := store.CreateWorkflow(ctx, "Agent settings reservation", user.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })
			providerNode := models.WorkflowNode{ID: "provider", Type: models.NodeTypeProvider, Template: "openai", KeyMode: "platform", Model: "gpt-4.1"}
			if mode == "byok" {
				providerNode.KeyMode = "byok"
				providerNode.APIKey = "own-test-key"
			}
			graph := models.WorkflowGraph{
				Nodes: []models.WorkflowNode{{ID: "start", Type: models.NodeTypeTrigger}, {ID: "agent", Type: models.NodeTypeAgent}, providerNode, {ID: "end", Type: models.NodeTypeEnd}},
				Edges: []models.WorkflowEdge{
					{ID: "start-agent", From: "start", To: "agent", Kind: models.EdgeKindFlow},
					{ID: "agent-end", From: "agent", To: "end", Kind: models.EdgeKindFlow},
					{ID: "provider-agent", From: "provider", To: "agent", Kind: models.EdgeKindAttach, ToPort: "model"},
				},
			}
			if mode == "prefund_rejected" {
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					quotes.Add(1)
					if r.Header.Get("X-PAYMENT") != "" || r.Header.Get("PAYMENT-SIGNATURE") != "" {
						payments.Add(1)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusPaymentRequired)
					_, _ = w.Write([]byte(`{"x402Version":2,"accepts":[{"scheme":"exact","network":"algorand:testnet","asset":"10458941","payTo":"recipient","amount":"200000","maxAmountRequired":"200000"}]}`))
				}))
				defer target.Close()
				graph.Nodes = append(graph.Nodes, models.WorkflowNode{ID: "paid", Type: models.NodeTypeTool402, Endpoint: target.URL, Method: "GET"})
				graph.Edges = append(graph.Edges, models.WorkflowEdge{ID: "paid-agent", From: "paid", To: "agent", Kind: models.EdgeKindAttach, ToPort: "tools"})
			}
			wf, err = store.UpdateWorkflow(ctx, wf.ID, wf.Name, graph)
			if err != nil {
				t.Fatal(err)
			}
			run, err := store.CreateRun(ctx, wf.ID, "test", []byte(`{"message":"hello"}`))
			if err != nil {
				t.Fatal(err)
			}
			runner.Start(wf, run)
			final := waitForRunDone(t, store, run.ID)
			blocked := mode == "over_limit" || mode == "insufficient" || mode == "prefund_rejected"
			wantStatus := models.RunStatusSuccess
			wantTurns := int32(1)
			if blocked {
				wantStatus = models.RunStatusFailed
				wantTurns = 0
			}
			if final.Status != wantStatus || turns.Load() != wantTurns {
				t.Fatalf("status=%s turns=%d, want %s/%d", final.Status, turns.Load(), wantStatus, wantTurns)
			}
			wantQuotes := int32(0)
			if mode == "prefund_rejected" {
				wantQuotes = 1
			}
			if quotes.Load() != wantQuotes || payments.Load() != 0 {
				t.Fatalf("quotes=%d payments=%d, want %d/0", quotes.Load(), payments.Load(), wantQuotes)
			}
			wantEntries := 0
			if !blocked && mode != "byok" {
				balance -= fee
				wantEntries = 1
			}
			remaining, err := store.GetCreditBalance(ctx, user.ID)
			if err != nil || remaining != balance {
				t.Fatalf("balance=%d error=%v, want %d", remaining, err, balance)
			}
			entries, err := store.ListDebitLedger(ctx, run.ID)
			if err != nil || len(entries) != wantEntries {
				t.Fatalf("ledger=%+v error=%v, want %d entries", entries, err, wantEntries)
			}
			if wantEntries == 1 && (entries[0].NodeID != "agent" || entries[0].AmountUSDMicros != fee || entries[0].Kind != models.DebitKindPlatformKeyLLMFee || entries[0].TokensIn == nil || *entries[0].TokensIn != 7 || entries[0].TokensOut == nil || *entries[0].TokensOut != 11) {
				t.Fatalf("unexpected agent charge: %+v", entries[0])
			}
		})
	}
}
