package db_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/db"
)

func TestChatSessionRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	wf, _ := store.CreateWorkflow(ctx, "Chat", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	msgs := json.RawMessage(`[{"id":"u-1","sender":"user","text":"hi","ts":"2026-09-15T00:00:00Z"}]`)
	if err := store.SaveChatSession(ctx, wf.ID, "sess1", msgs); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetChatSession(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "sess1" {
		t.Errorf("session id: want sess1 got %q", got.SessionID)
	}
	if !strings.Contains(string(got.Messages), `"hi"`) {
		t.Errorf("transcript did not survive: %s", got.Messages)
	}

	// A second save replaces the transcript rather than appending a row.
	if err := store.SaveChatSession(ctx, wf.ID, "sess1",
		json.RawMessage(`[{"id":"u-1","sender":"user","text":"hi"},{"id":"a-1","sender":"assistant","text":"hello"}]`)); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetChatSession(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(got.Messages, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 messages after replace, got %d", len(out))
	}
}

func TestChatSessionEmptyForNewWorkflow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	wf, _ := store.CreateWorkflow(ctx, "Chat", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	got, err := store.GetChatSession(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(got.Messages, &out); err != nil {
		t.Fatalf("a workflow with no conversation must still decode as a list: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("want no messages, got %d", len(out))
	}
}

func TestSaveChatSessionRejectsOversizedAndNonList(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	wf, _ := store.CreateWorkflow(ctx, "Chat", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	huge := json.RawMessage(`["` + strings.Repeat("x", db.MaxChatTranscriptBytes) + `"]`)
	if err := store.SaveChatSession(ctx, wf.ID, "s", huge); err == nil {
		t.Error("an oversized transcript must be refused")
	}
	if err := store.SaveChatSession(ctx, wf.ID, "s", json.RawMessage(`{"not":"a list"}`)); err == nil {
		t.Error("a transcript that is not a list must be refused")
	}
	if err := store.SaveChatSession(ctx, wf.ID, "s", json.RawMessage(`not json`)); err == nil {
		t.Error("invalid JSON must be refused")
	}
}

// The transcript belongs to the workflow: deleting one takes the other.
func TestChatSessionGoesWithTheWorkflow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	wf, _ := store.CreateWorkflow(ctx, "Chat", "dev")
	if err := store.SaveChatSession(ctx, wf.ID, "s", json.RawMessage(`[{"id":"u-1"}]`)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteWorkflow(ctx, wf.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetChatSession(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	json.Unmarshal(got.Messages, &out)
	if len(out) != 0 {
		t.Fatalf("transcript outlived its workflow: %s", got.Messages)
	}
}
