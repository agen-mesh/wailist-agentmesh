package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/wallet"
)

func TestDeploy(t *testing.T) {
	d := testDeps(t)
	d.Wallet = wallet.NewService(
		"0123456789abcdef0123456789abcdef",
		"https://testnet-api.algonode.cloud", "", "testnet",
	)
	d.BaseURL = "http://localhost:8080"

	ctx := context.Background()
	wf, _ := d.Store.CreateWorkflow(ctx, "Deploy Test", "dev")
	t.Cleanup(func() { d.Store.DeleteWorkflow(ctx, wf.ID) })

	graph := models.WorkflowGraph{
		Nodes: []models.WorkflowNode{
			{ID: "n1", Type: models.NodeTypeTrigger},
			{ID: "n2", Type: models.NodeTypeAgent, Name: "My Agent"},
		},
		Edges: []models.WorkflowEdge{{ID: "e1", From: "n1", To: "n2", Kind: models.EdgeKindFlow}},
	}
	d.Store.UpdateWorkflow(ctx, wf.ID, "Deploy Test", graph)

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
		t.Fatalf("want 1 agent wallet got %d", len(agents))
	}
}
