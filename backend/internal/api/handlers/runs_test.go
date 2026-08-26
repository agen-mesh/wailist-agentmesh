package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
)

func TestTriggerRun(t *testing.T) {
	d := testDeps(t)
	d.Broker = sse.NewBroker()
	d.Wallet = wallet.NewService("0123456789abcdef0123456789abcdef",
		"https://testnet-api.algonode.cloud", "", "testnet")
	d.Engine = engine.NewRunner(d.Store, d.Broker, d.Wallet, "http://localhost:8080", "", "", engine.X402Config{USDCAssetID: 10458941})

	wf, _ := d.Store.CreateWorkflow(t.Context(), "Run Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	body, _ := json.Marshal(map[string]string{"message": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/run", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	req = withURLParam(req, "id", wf.ID)
	w := httptest.NewRecorder()
	d.TriggerRun(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202 got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["runId"] == "" {
		t.Fatal("no runId")
	}
}

// TestPublicTriggerRejectsNonWebhookTriggerEvenIfDeployed guards the actual
// security fix: the public /run/{workflowId} path used to accept ANY
// deployed workflow with a trigger node of any template, so a "manual" or
// "chat" trigger workflow could be fired unauthenticated the same way a
// "webhook" one legitimately can. It must now require the deployed
// workflow's trigger node be specifically template "webhook".
func TestPublicTriggerRejectsNonWebhookTriggerEvenIfDeployed(t *testing.T) {
	d := testDeps(t)
	d.Broker = sse.NewBroker()
	d.Wallet = wallet.NewService(testEncryptionKey, "https://testnet-api.algonode.cloud", "", "testnet")
	d.Engine = engine.NewRunner(d.Store, d.Broker, d.Wallet, "http://localhost:8080", "", "", engine.X402Config{USDCAssetID: 10458941})
	d.BaseURL = "http://localhost:8080"

	ctx := context.Background()
	wf, _ := d.Store.CreateWorkflow(ctx, "Manual Trigger Public Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger, Template: "manual"},
			{ID: "n2", Type: models.NodeTypeAgent, Name: "My Agent"},
		},
		Edges: []models.WorkflowEdge{{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow}},
	}
	if _, err := d.Store.UpdateWorkflow(ctx, wf.ID, "Manual Trigger Public Test", graph); err != nil {
		t.Fatal(err)
	}

	deployReq := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/deploy", nil)
	deployReq = deployReq.WithContext(context.WithValue(deployReq.Context(), handlers.CtxUserID, "dev"))
	deployReq = withURLParam(deployReq, "id", wf.ID)
	dw := httptest.NewRecorder()
	d.Deploy(dw, deployReq)
	if dw.Code != http.StatusOK {
		t.Fatalf("deploy: want 200 got %d body=%s", dw.Code, dw.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/run/"+wf.ID, bytes.NewReader([]byte("{}")))
	req = withURLParam(req, "workflowId", wf.ID)
	w := httptest.NewRecorder()
	d.PublicTrigger(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("manual-trigger workflow via public endpoint: want 404 got %d body=%s", w.Code, w.Body.String())
	}
}

// TestPublicTriggerRequiresCorrectWebhookSecret covers the other half of the
// fix: a "webhook"-trigger workflow is still publicly reachable (that's the
// feature), but only with the secret generated for it -- missing or wrong
// secrets are rejected, and the right one starts a real run.
func TestPublicTriggerRequiresCorrectWebhookSecret(t *testing.T) {
	d := testDeps(t)
	d.Broker = sse.NewBroker()
	d.Wallet = wallet.NewService(testEncryptionKey, "https://testnet-api.algonode.cloud", "", "testnet")
	d.Engine = engine.NewRunner(d.Store, d.Broker, d.Wallet, "http://localhost:8080", "", "", engine.X402Config{USDCAssetID: 10458941})
	d.BaseURL = "http://localhost:8080"

	ctx := context.Background()
	wf, _ := d.Store.CreateWorkflow(ctx, "Webhook Public Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	// Saved through the HTTP handler (not d.Store.UpdateWorkflow directly)
	// so ensureWebhookSecrets actually runs and generates a real secret.
	body, _ := json.Marshal(map[string]any{
		"name": "Webhook Public Test",
		"nodes": []map[string]any{
			{"id": "n1", "type": "trigger", "template": "webhook"},
			{"id": "n2", "type": "agent", "name": "My Agent"},
		},
		"edges": []map[string]any{{"id": "e1", "from": "n1", "to": "n2", "kind": "flow"}},
	})
	saveReq := httptest.NewRequest(http.MethodPut, "/workflows/"+wf.ID, bytes.NewReader(body))
	saveReq = saveReq.WithContext(context.WithValue(saveReq.Context(), handlers.CtxUserID, "dev"))
	saveReq = withURLParam(saveReq, "id", wf.ID)
	sw := httptest.NewRecorder()
	d.UpdateWorkflow(sw, saveReq)
	if sw.Code != http.StatusOK {
		t.Fatalf("save: want 200 got %d body=%s", sw.Code, sw.Body.String())
	}
	var saved models.Workflow
	json.NewDecoder(sw.Body).Decode(&saved)
	secret := saved.Nodes[0].Secrets["webhookSecret"]
	if secret == "" {
		t.Fatal("expected a webhookSecret to be generated")
	}

	deployReq := httptest.NewRequest(http.MethodPost, "/workflows/"+wf.ID+"/deploy", nil)
	deployReq = deployReq.WithContext(context.WithValue(deployReq.Context(), handlers.CtxUserID, "dev"))
	deployReq = withURLParam(deployReq, "id", wf.ID)
	dw := httptest.NewRecorder()
	d.Deploy(dw, deployReq)
	if dw.Code != http.StatusOK {
		t.Fatalf("deploy: want 200 got %d body=%s", dw.Code, dw.Body.String())
	}

	// Missing secret -> 404, same as a nonexistent workflow (no leak).
	noneReq := httptest.NewRequest(http.MethodPost, "/run/"+wf.ID, bytes.NewReader([]byte("{}")))
	noneReq = withURLParam(noneReq, "workflowId", wf.ID)
	nw := httptest.NewRecorder()
	d.PublicTrigger(nw, noneReq)
	if nw.Code != http.StatusNotFound {
		t.Fatalf("missing secret: want 404 got %d body=%s", nw.Code, nw.Body.String())
	}

	// Wrong secret -> 404.
	badReq := httptest.NewRequest(http.MethodPost, "/run/"+wf.ID, bytes.NewReader([]byte("{}")))
	badReq.Header.Set("X-Webhook-Secret", "wrong-secret")
	badReq = withURLParam(badReq, "workflowId", wf.ID)
	bw := httptest.NewRecorder()
	d.PublicTrigger(bw, badReq)
	if bw.Code != http.StatusNotFound {
		t.Fatalf("wrong secret: want 404 got %d body=%s", bw.Code, bw.Body.String())
	}

	// Correct secret -> 202, a real run starts.
	goodReq := httptest.NewRequest(http.MethodPost, "/run/"+wf.ID, bytes.NewReader([]byte("{}")))
	goodReq.Header.Set("X-Webhook-Secret", secret)
	goodReq = withURLParam(goodReq, "workflowId", wf.ID)
	gw := httptest.NewRecorder()
	d.PublicTrigger(gw, goodReq)
	if gw.Code != http.StatusAccepted {
		t.Fatalf("correct secret: want 202 got %d body=%s", gw.Code, gw.Body.String())
	}
	var runResp map[string]string
	json.NewDecoder(gw.Body).Decode(&runResp)
	if runResp["runId"] == "" {
		t.Fatal("no runId")
	}
}
