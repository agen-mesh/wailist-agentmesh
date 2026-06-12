package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/wallet"
)

// TestDeployGeneratesWebhookURL verifies that deploying a workflow whose first node
// is a webhook trigger generates and returns a WebhookLiveURL.
func TestDeployGeneratesWebhookURL(t *testing.T) {
	d := testDeps(t)
	d.Wallet = wallet.NewService("0123456789abcdef0123456789abcdef", "https://testnet-api.algonode.cloud", "", "testnet")
	d.BaseURL = "http://localhost:8080"

	ctx := context.Background()
	wf, _ := d.Store.CreateWorkflow(ctx, "Webhook Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(ctx, wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "t1", Type: models.NodeTypeTrigger, Template: "webhook"},
			{ID: "a1", Type: models.NodeTypeAgent, Name: "Handler"},
		},
		Edges: []models.WorkflowEdge{
			{ID: "e1", From: "t1", To: "a1", Kind: models.EdgeKindFlow},
		},
	}
	d.Store.UpdateWorkflow(ctx, wf.ID, "Webhook Test", graph)

	req := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/deploy", nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	req = withURLParam(req, "id", wf.ID)
	w := httptest.NewRecorder()
	d.Deploy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	webhooks, _ := resp["webhooks"].([]any)
	if len(webhooks) != 1 {
		t.Fatalf("want 1 webhook result, got %d: %v", len(webhooks), webhooks)
	}
	wh := webhooks[0].(map[string]any)
	url, _ := wh["url"].(string)
	if !strings.HasPrefix(url, "/hooks/") {
		t.Fatalf("webhook URL should start with /hooks/, got %q", url)
	}
	if !strings.Contains(url, wf.ID) {
		t.Fatalf("webhook URL should contain workflow ID %q, got %q", wf.ID, url)
	}
	method, _ := wh["method"].(string)
	if method != "POST" {
		t.Fatalf("want default method POST, got %q", method)
	}
}

// TestDeployWebhookRespectsCustomMethod verifies that a node with WebhookMethod="GET"
// keeps that method in the generated URL response.
func TestDeployWebhookRespectsCustomMethod(t *testing.T) {
	d := testDeps(t)
	d.Wallet = wallet.NewService("0123456789abcdef0123456789abcdef", "https://testnet-api.algonode.cloud", "", "testnet")
	d.BaseURL = "http://localhost:8080"

	ctx := context.Background()
	wf, _ := d.Store.CreateWorkflow(ctx, "Webhook Method Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(ctx, wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "t1", Type: models.NodeTypeTrigger, Template: "webhook", WebhookMethod: "GET"},
		},
	}
	d.Store.UpdateWorkflow(ctx, wf.ID, "Webhook Method Test", graph)

	req := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/deploy", nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	req = withURLParam(req, "id", wf.ID)
	w := httptest.NewRecorder()
	d.Deploy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	webhooks, _ := resp["webhooks"].([]any)
	if len(webhooks) != 1 {
		t.Fatalf("want 1 webhook, got %d", len(webhooks))
	}
	method, _ := webhooks[0].(map[string]any)["method"].(string)
	if method != "GET" {
		t.Fatalf("want method GET, got %q", method)
	}
}

// TestDeployNonWebhookTriggerNoURL verifies that non-webhook triggers (cron, chat)
// do NOT generate a webhook URL.
func TestDeployNonWebhookTriggerNoURL(t *testing.T) {
	d := testDeps(t)
	d.Wallet = wallet.NewService("0123456789abcdef0123456789abcdef", "https://testnet-api.algonode.cloud", "", "testnet")
	d.BaseURL = "http://localhost:8080"

	ctx := context.Background()
	wf, _ := d.Store.CreateWorkflow(ctx, "Cron Trigger Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(ctx, wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "t1", Type: models.NodeTypeTrigger, Template: "cron"},
		},
	}
	d.Store.UpdateWorkflow(ctx, wf.ID, "Cron Trigger Test", graph)

	req := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/deploy", nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	req = withURLParam(req, "id", wf.ID)
	w := httptest.NewRecorder()
	d.Deploy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	webhooks, _ := resp["webhooks"].([]any)
	if len(webhooks) != 0 {
		t.Fatalf("cron trigger should not produce webhook URL, got %d", len(webhooks))
	}
}

// TestDeployPlatformWalletUsedWhenEnvSet verifies that when PLATFORM_ALGO_MNEMONIC
// is set and the agent has SelfFundWallet=false, the deploy returns a wallet address
// (derived from the mnemonic) and marks it as platform-funded.
func TestDeployPlatformWalletUsedWhenEnvSet(t *testing.T) {
	svc := wallet.NewService("0123456789abcdef0123456789abcdef", "https://testnet-api.algonode.cloud", "", "testnet")

	// Generate a fresh wallet to get a valid mnemonic — we never hard-code mnemonics.
	_, enc, err := svc.GenerateWallet()
	if err != nil {
		t.Fatal(err)
	}
	mn, err := svc.DecryptMnemonic(enc)
	if err != nil {
		t.Fatal(err)
	}

	os.Setenv("PLATFORM_ALGO_MNEMONIC", mn)
	defer os.Unsetenv("PLATFORM_ALGO_MNEMONIC")

	d := testDeps(t)
	d.Wallet = svc
	d.BaseURL = "http://localhost:8080"

	ctx := context.Background()
	wf, _ := d.Store.CreateWorkflow(ctx, "Platform Wallet Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(ctx, wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			// SelfFundWallet=false → should use platform wallet
			{ID: "a1", Type: models.NodeTypeAgent, Name: "Agent", SelfFundWallet: false},
		},
	}
	d.Store.UpdateWorkflow(ctx, wf.ID, "Platform Wallet Test", graph)

	req := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/deploy", nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	req = withURLParam(req, "id", wf.ID)
	w := httptest.NewRecorder()
	d.Deploy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	agents, _ := resp["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(agents))
	}
	agent := agents[0].(map[string]any)
	if agent["address"] == "" || agent["address"] == nil {
		t.Fatal("expected a wallet address for platform-funded agent")
	}
	if agent["platformFunded"] != true {
		t.Fatalf("expected platformFunded=true, got %v", agent["platformFunded"])
	}
}

// TestDeploySelfFundWalletGeneratesNewWallet verifies that SelfFundWallet=true
// generates a fresh per-agent wallet (not the platform wallet).
func TestDeploySelfFundWalletGeneratesNewWallet(t *testing.T) {
	svc := wallet.NewService("0123456789abcdef0123456789abcdef", "https://testnet-api.algonode.cloud", "", "testnet")

	// Set a platform mnemonic — it should NOT be used for self-funded agents.
	_, enc, _ := svc.GenerateWallet()
	platformMn, _ := svc.DecryptMnemonic(enc)
	platformAddr, _, _ := svc.WrapMnemonic(platformMn)

	os.Setenv("PLATFORM_ALGO_MNEMONIC", platformMn)
	defer os.Unsetenv("PLATFORM_ALGO_MNEMONIC")

	d := testDeps(t)
	d.Wallet = svc
	d.BaseURL = "http://localhost:8080"

	ctx := context.Background()
	wf, _ := d.Store.CreateWorkflow(ctx, "Self Fund Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(ctx, wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			// SelfFundWallet=true → fresh keypair, not platform wallet
			{ID: "a1", Type: models.NodeTypeAgent, Name: "Agent", SelfFundWallet: true},
		},
	}
	d.Store.UpdateWorkflow(ctx, wf.ID, "Self Fund Test", graph)

	req := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/deploy", nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	req = withURLParam(req, "id", wf.ID)
	w := httptest.NewRecorder()
	d.Deploy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	agents, _ := resp["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(agents))
	}
	agent := agents[0].(map[string]any)
	addr, _ := agent["address"].(string)
	if addr == platformAddr {
		t.Fatal("self-funded agent must NOT use the platform wallet address")
	}
	if agent["platformFunded"] == true {
		t.Fatal("self-funded agent must not be marked platformFunded")
	}
}
