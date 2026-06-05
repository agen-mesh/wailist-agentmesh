package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
)

func testDeps(t *testing.T) *handlers.Deps {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return &handlers.Deps{Store: store}
}

func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCreateAndGetWorkflow(t *testing.T) {
	d := testDeps(t)

	body, _ := json.Marshal(map[string]string{"name": "My WF"})
	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	w := httptest.NewRecorder()
	d.CreateWorkflow(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: want 201 got %d body=%s", w.Code, w.Body.String())
	}

	var wf models.Workflow
	json.NewDecoder(w.Body).Decode(&wf)
	if wf.ID == "" {
		t.Fatal("no id")
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	req2 := httptest.NewRequest(http.MethodGet, "/workflows/"+wf.ID, nil)
	req2 = withURLParam(req2, "id", wf.ID)
	w2 := httptest.NewRecorder()
	d.GetWorkflow(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get: want 200 got %d", w2.Code)
	}
}
