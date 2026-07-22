package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCreditTransactionLifecycle(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("credit-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	orderID := fmt.Sprintf("order_test_%d", time.Now().UnixNano())
	txn, err := store.CreateCreditTransaction(ctx, user.ID, orderID, 50000, 0.012)
	if err != nil {
		t.Fatal(err)
	}
	if txn.Status != "pending" {
		t.Fatalf("want pending got %s", txn.Status)
	}
	wantMicros := int64(50000.0 / 100.0 * 0.012 * 1e6)
	if txn.CreditUSDMicros != wantMicros {
		t.Fatalf("want %d got %d", wantMicros, txn.CreditUSDMicros)
	}

	credited, err := store.CompleteCreditTransaction(ctx, orderID, "pay_test_1")
	if err != nil {
		t.Fatal(err)
	}
	if credited != wantMicros {
		t.Fatalf("want %d got %d", wantMicros, credited)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != wantMicros {
		t.Fatalf("want balance %d got %d", wantMicros, balance)
	}

	// Replay must not double-credit.
	credited2, err := store.CompleteCreditTransaction(ctx, orderID, "pay_test_1")
	if err != nil {
		t.Fatal(err)
	}
	if credited2 != wantMicros {
		t.Fatalf("replay: want %d got %d", wantMicros, credited2)
	}
	balance2, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance2 != wantMicros {
		t.Fatalf("replay must not double-credit: want %d got %d", wantMicros, balance2)
	}
}

func TestExpireStalePendingTransactions(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("credit-expire-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	orderID := fmt.Sprintf("order_expire_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, user.ID, orderID, 10000, 0.012); err != nil {
		t.Fatal(err)
	}

	// Row is only a few milliseconds old — a 24h threshold must not touch it.
	n, err := store.ExpireStalePendingTransactions(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 rows expired (too fresh), got %d", n)
	}

	// A near-zero threshold makes the row qualify as stale.
	n2, err := store.ExpireStalePendingTransactions(ctx, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if n2 < 1 {
		t.Fatalf("want at least 1 row expired, got %d", n2)
	}

	// Re-running must not re-touch rows that are no longer 'pending'.
	n3, err := store.ExpireStalePendingTransactions(ctx, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if n3 != 0 {
		t.Fatalf("want 0 rows on second sweep (already expired), got %d", n3)
	}
}
