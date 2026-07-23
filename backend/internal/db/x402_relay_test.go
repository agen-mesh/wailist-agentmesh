package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/db"
)

func newTestStore(t *testing.T) *db.Store {
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
	return store
}

func TestRecordInboundSettlementThenOutbound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	row, err := store.RecordInboundSettlement(ctx, "https://example.com/paid", "INBOUND-TX-1", 100000)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "pending_outbound" {
		t.Fatalf("want pending_outbound, got %s", row.Status)
	}

	if err := store.RecordOutboundSettlement(ctx, row.ID, "OUTBOUND-TX-1", "settled"); err != nil {
		t.Fatal(err)
	}
}

func TestRecordInboundSettlementRejectsReplay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.RecordInboundSettlement(ctx, "https://example.com/paid", "INBOUND-TX-REPLAY", 100000); err != nil {
		t.Fatal(err)
	}
	_, err := store.RecordInboundSettlement(ctx, "https://example.com/paid", "INBOUND-TX-REPLAY", 100000)
	if err != db.ErrDuplicateSettlement {
		t.Fatalf("want ErrDuplicateSettlement, got %v", err)
	}
}
