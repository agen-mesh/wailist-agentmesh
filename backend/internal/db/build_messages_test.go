package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/db"
)

func TestBuildMessagesRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	wf, _ := store.CreateWorkflow(ctx, "Builder", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	if err := store.AppendBuildMessage(ctx, wf.ID, "user", "build a phone search agent"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendBuildMessage(ctx, wf.ID, "model", "Built it."); err != nil {
		t.Fatal(err)
	}

	msgs, err := store.GetBuildMessages(ctx, wf.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	// Oldest first: the slice is replayed straight into the model's contents,
	// where reversed turns would read as the assistant speaking first.
	if msgs[0].Role != "user" || msgs[0].Text != "build a phone search agent" {
		t.Fatalf("first message wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "model" {
		t.Fatalf("second message wrong: %+v", msgs[1])
	}
}

func TestGetBuildMessagesReturnsMostRecentWithinLimit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	wf, _ := store.CreateWorkflow(ctx, "Builder", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	for i := 0; i < 10; i++ {
		if err := store.AppendBuildMessage(ctx, wf.ID, "user", string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := store.GetBuildMessages(ctx, wf.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("want 3, got %d", len(msgs))
	}
	// The three most recent, still oldest-first among themselves.
	if msgs[0].Text != "h" || msgs[2].Text != "j" {
		t.Fatalf("want h,i,j got %q,%q,%q", msgs[0].Text, msgs[1].Text, msgs[2].Text)
	}
}

func TestGetBuildMessagesEmptyForNewWorkflow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	wf, _ := store.CreateWorkflow(ctx, "Builder", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	msgs, err := store.GetBuildMessages(ctx, wf.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("want none, got %d", len(msgs))
	}
}

func TestAppendBuildMessageRejectsUnknownRole(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	wf, _ := store.CreateWorkflow(ctx, "Builder", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	if err := store.AppendBuildMessage(ctx, wf.ID, "system", "nope"); err == nil {
		t.Fatal("expected an error for an unknown role")
	}
}

// Oversized text is truncated rather than rejected: losing the turn entirely
// would break the conversation, and the DB CHECK would reject the insert.
func TestAppendBuildMessageTruncatesOversizedText(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	wf, _ := store.CreateWorkflow(ctx, "Builder", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	if err := store.AppendBuildMessage(ctx, wf.ID, "user", strings.Repeat("x", 40_000)); err != nil {
		t.Fatal(err)
	}
	msgs, err := store.GetBuildMessages(ctx, wf.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || len(msgs[0].Text) > db.MaxBuildMessageChars {
		t.Fatalf("want text truncated to <= %d, got %d", db.MaxBuildMessageChars, len(msgs[0].Text))
	}
}

// Truncation must land on a character boundary. The DB CHECK counts
// characters (char_length), and slicing a Go string by bytes can split a
// multi-byte rune, leaving invalid UTF-8 that Postgres rejects outright --
// the exact insert failure truncation exists to prevent.
func TestAppendBuildMessageTruncatesMultibyteTextSafely(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	wf, _ := store.CreateWorkflow(ctx, "Builder", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	// "é" is two bytes, so a byte-slice at MaxBuildMessageChars would cut
	// one clean in half.
	if err := store.AppendBuildMessage(ctx, wf.ID, "user", strings.Repeat("é", 20_000)); err != nil {
		t.Fatalf("a long multi-byte message must still be stored: %v", err)
	}
	msgs, err := store.GetBuildMessages(ctx, wf.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(msgs[0].Text)); n != db.MaxBuildMessageChars {
		t.Fatalf("want exactly %d characters kept, got %d", db.MaxBuildMessageChars, n)
	}
}

func TestClearBuildMessages(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	wf, _ := store.CreateWorkflow(ctx, "Builder", "dev")
	t.Cleanup(func() { store.DeleteWorkflow(ctx, wf.ID) })

	if err := store.AppendBuildMessage(ctx, wf.ID, "user", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearBuildMessages(ctx, wf.ID); err != nil {
		t.Fatal(err)
	}
	msgs, _ := store.GetBuildMessages(ctx, wf.ID, 20)
	if len(msgs) != 0 {
		t.Fatalf("want none after clear, got %d", len(msgs))
	}
}
