package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
)

func chatRequest(t *testing.T, method, workflowID, userID, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, "/workflows/"+workflowID+"/chat", strings.NewReader(body))
	r = withURLParam(r, "id", workflowID)
	return r.WithContext(context.WithValue(r.Context(), handlers.CtxUserID, userID))
}

func TestChatSessionSavesAndLoads(t *testing.T) {
	d := testDeps(t)
	wf, err := d.Store.CreateWorkflow(t.Context(), "Chat", "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	body := `{"sessionId":"s1","messages":[{"id":"u-1","sender":"user","text":"price of myrad"}]}`
	w := httptest.NewRecorder()
	d.SaveChatSession(w, chatRequest(t, http.MethodPut, wf.ID, "dev", body))
	if w.Code != http.StatusNoContent {
		t.Fatalf("save: want 204 got %d (%s)", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	d.GetChatSession(w, chatRequest(t, http.MethodGet, wf.ID, "dev", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("load: want 200 got %d", w.Code)
	}
	var got db.ChatSession
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "s1" || !strings.Contains(string(got.Messages), "price of myrad") {
		t.Fatalf("transcript did not round-trip: %s", w.Body)
	}
}

// The transcript is as private as the workflow it belongs to.
func TestChatSessionOtherUserGets404(t *testing.T) {
	d := testDeps(t)
	wf, err := d.Store.CreateWorkflow(t.Context(), "Chat", "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	if err := d.Store.SaveChatSession(t.Context(), wf.ID, "s1",
		json.RawMessage(`[{"id":"u-1","text":"secret"}]`)); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	d.GetChatSession(w, chatRequest(t, http.MethodGet, wf.ID, "intruder", ""))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("another user's transcript leaked")
	}
	w = httptest.NewRecorder()
	d.SaveChatSession(w, chatRequest(t, http.MethodPut, wf.ID, "intruder", `{"sessionId":"x","messages":[]}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 on write got %d", w.Code)
	}
}

func TestChatSessionRejectsAnOversizedTranscript(t *testing.T) {
	d := testDeps(t)
	wf, err := d.Store.CreateWorkflow(t.Context(), "Chat", "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Store.DeleteWorkflow(context.Background(), wf.ID) })

	var buf bytes.Buffer
	buf.WriteString(`{"sessionId":"s","messages":["`)
	buf.WriteString(strings.Repeat("x", db.MaxChatTranscriptBytes))
	buf.WriteString(`"]}`)
	w := httptest.NewRecorder()
	d.SaveChatSession(w, chatRequest(t, http.MethodPut, wf.ID, "dev", buf.String()))
	if w.Code == http.StatusNoContent {
		t.Fatal("an oversized transcript was accepted")
	}
}
