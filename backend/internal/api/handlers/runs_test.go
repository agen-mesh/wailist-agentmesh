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
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
)

func TestTriggerRun(t *testing.T) {
	d := testDeps(t)
	d.Broker = sse.NewBroker()
	d.Engine = engine.NewRunner(d.Store, d.Broker)
	d.Wallet = wallet.NewService("0123456789abcdef0123456789abcdef",
		"https://testnet-api.algonode.cloud", "", "testnet")

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
