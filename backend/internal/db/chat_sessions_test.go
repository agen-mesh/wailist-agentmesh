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
	if err := store.SaveChatSession(ctx, wf.ID, db.ChatModeRun, "sess1", msgs); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetChatSession(ctx, wf.ID, db.ChatModeRun)
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
	if err := store.SaveChatSession(ctx, wf.ID, db.ChatModeRun, "sess1",
		json.RawMessage(`[{"id":"u-1","sender":"user","text":"hi"},{"id":"a-1","sender":"assistant","text":"hello"}]`)); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetChatSession(ctx, wf.ID, db.ChatModeRun)
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

	got, err := store.GetChatSession(ctx, wf.ID, db.ChatModeRun)
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
	if err := store.SaveChatSession(ctx, wf.ID, db.ChatModeRun, "s", huge); err == nil {
		t.Error("an oversized transcript must be refused")
	}
	if err := store.SaveChatSession(ctx, wf.ID, db.ChatModeRun, "s", json.RawMessage(`{"not":"a list"}`)); err == nil {
		t.Error("a transcript that is not a list must be refused")
	}
	if err := store.SaveChatSession(ctx, wf.ID, db.ChatModeRun, "s", json.RawMessage(`not json`)); err == nil {
		t.Error("invalid JSON must be refused")
	}
}

// The transcript belongs to the workflow: deleting one takes the other.
func TestChatSessionGoesWithTheWorkflow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	wf, _ := store.CreateWorkflow(ctx, "Chat", "dev")
	if err := store.SaveChatSession(ctx, wf.ID, db.ChatModeRun, "s", json.RawMessage(`[{"id":"u-1"}]`)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteWorkflow(ctx, wf.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetChatSession(ctx, wf.ID, db.ChatModeRun)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	json.Unmarshal(got.Messages, &out)
	if len(out) != 0 {
		t.Fatalf("transcript outlived its workflow: %s", got.Messages)
	}
}

// Building a workflow and talking to the finished one are two conversations.
func TestChatSessionKeepsBuildAndRunApart(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	wf, _ := store.CreateWorkflow(ctx, "Chat", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	if err := store.SaveChatSession(ctx, wf.ID, db.ChatModeBuild, "b",
		json.RawMessage(`[{"id":"u-1","text":"add a coingecko step"}]`)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveChatSession(ctx, wf.ID, db.ChatModeRun, "r",
		json.RawMessage(`[{"id":"u-2","text":"what is bitcoin worth"}]`)); err != nil {
		t.Fatal(err)
	}
	build, err := store.GetChatSession(ctx, wf.ID, db.ChatModeBuild)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.GetChatSession(ctx, wf.ID, db.ChatModeRun)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(build.Messages), "coingecko") || strings.Contains(string(build.Messages), "bitcoin") {
		t.Errorf("build transcript is not its own: %s", build.Messages)
	}
	if !strings.Contains(string(run.Messages), "bitcoin") || strings.Contains(string(run.Messages), "coingecko") {
		t.Errorf("run transcript is not its own: %s", run.Messages)
	}
	if build.SessionID == run.SessionID {
		t.Error("each mode has its own session id")
	}
}

func TestParseChatMode(t *testing.T) {
	for _, in := range []string{"build", "run", ""} {
		if _, ok := db.ParseChatMode(in); !ok {
			t.Errorf("%q must be accepted", in)
		}
	}
	if m, _ := db.ParseChatMode(""); m != db.ChatModeRun {
		t.Error("an absent mode defaults to run")
	}
	if _, ok := db.ParseChatMode("../build"); ok {
		t.Error("anything else must be refused")
	}
}
