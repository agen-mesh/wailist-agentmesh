package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/wallet"
)

const testEncryptionKey = "0123456789abcdef0123456789abcdef"

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
	return &handlers.Deps{Store: store, EncryptionKey: testEncryptionKey}
}

func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// TestAPIKeyEncryption verifies the full encrypt → mask → decrypt lifecycle:
//   - Saving a plaintext key stores an encrypted blob in the DB
//   - GET and UpdateWorkflow responses always return the sentinel, never plaintext or raw cipher
//   - Re-saving with the sentinel preserves the existing encrypted blob unchanged
//   - The encrypted DB value decrypts back to the original key
func TestAPIKeyEncryption(t *testing.T) {
	d := testDeps(t)

	wf, err := d.Store.CreateWorkflow(t.Context(), "SecretTest", "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	// ── 1. Save a workflow with a plaintext API key ──────────────────────────
	body, _ := json.Marshal(map[string]any{
		"name": "SecretTest",
		"nodes": []map[string]any{
			{"id": "n1", "type": "provider", "template": "gemini", "apiKey": "my-test-api-key-123"},
		},
		"edges": []any{},
	})
	req := httptest.NewRequest(http.MethodPut, "/workflows/"+wf.ID, bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, "dev"))
	req = withURLParam(req, "id", wf.ID)
	w := httptest.NewRecorder()
	d.UpdateWorkflow(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: want 200 got %d body=%s", w.Code, w.Body.String())
	}

	var updated models.Workflow
	json.NewDecoder(w.Body).Decode(&updated)
	apiKeyInResponse := ""
	for _, n := range updated.Nodes {
		if n.ID == "n1" {
			apiKeyInResponse = n.APIKey
		}
	}
	if apiKeyInResponse != handlers.EncSentinel {
		t.Errorf("UpdateWorkflow response: want %q got %q", handlers.EncSentinel, apiKeyInResponse)
	}

	// ── 2. GET must also return the sentinel, never plaintext ────────────────
	req2 := httptest.NewRequest(http.MethodGet, "/workflows/"+wf.ID, nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), handlers.CtxUserID, "dev"))
	req2 = withURLParam(req2, "id", wf.ID)
	w2 := httptest.NewRecorder()
	d.GetWorkflow(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get: want 200 got %d", w2.Code)
	}
	var got models.Workflow
	json.NewDecoder(w2.Body).Decode(&got)
	for _, n := range got.Nodes {
		if n.ID == "n1" && n.APIKey != handlers.EncSentinel {
			t.Errorf("GET response: want %q got %q", handlers.EncSentinel, n.APIKey)
		}
	}

	// ── 3. Raw DB value must be an encrypted blob, not plaintext ────────────
	rawWF, err := d.Store.GetWorkflow(t.Context(), wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	var rawKey string
	for _, n := range rawWF.Nodes {
		if n.ID == "n1" {
			rawKey = n.APIKey
		}
	}
	if !strings.HasPrefix(rawKey, "enc:") {
		t.Errorf("DB value: want enc: prefix, got %q", rawKey)
	}

	// ── 4. DB blob decrypts back to the original key ─────────────────────────
	plain, err := wallet.Decrypt(strings.TrimPrefix(rawKey, "enc:"), testEncryptionKey)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if plain != "my-test-api-key-123" {
		t.Errorf("decrypted: want %q got %q", "my-test-api-key-123", plain)
	}

	// ── 5. Re-saving with the sentinel keeps the existing encrypted blob ─────
	body2, _ := json.Marshal(map[string]any{
		"name": "SecretTest",
		"nodes": []map[string]any{
			{"id": "n1", "type": "provider", "template": "gemini", "apiKey": handlers.EncSentinel},
		},
		"edges": []any{},
	})
	req3 := httptest.NewRequest(http.MethodPut, "/workflows/"+wf.ID, bytes.NewReader(body2))
	req3 = req3.WithContext(context.WithValue(req3.Context(), handlers.CtxUserID, "dev"))
	req3 = withURLParam(req3, "id", wf.ID)
	w3 := httptest.NewRecorder()
	d.UpdateWorkflow(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("re-save sentinel: want 200 got %d", w3.Code)
	}
	rawWF2, _ := d.Store.GetWorkflow(t.Context(), wf.ID)
	for _, n := range rawWF2.Nodes {
		if n.ID == "n1" && n.APIKey != rawKey {
			t.Errorf("sentinel re-save: encrypted blob changed; was %q, now %q", rawKey, n.APIKey)
		}
	}
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
	req2 = req2.WithContext(context.WithValue(req2.Context(), handlers.CtxUserID, "dev"))
	req2 = withURLParam(req2, "id", wf.ID)
	w2 := httptest.NewRecorder()
	d.GetWorkflow(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get: want 200 got %d", w2.Code)
	}
}
