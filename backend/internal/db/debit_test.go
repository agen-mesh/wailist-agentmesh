package db_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
)

// fundUser tops up userID to exactly micros USD-micros via the existing
// top-up path (amount_inr_paise=100, fx_rate chosen so credit_usd_micros
// lands on the requested value), so debit tests start from a known balance.
func fundUser(t *testing.T, store *db.Store, userID string, micros int64) {
	t.Helper()
	ctx := context.Background()
	orderID := fmt.Sprintf("fund_%s_%d", userID, time.Now().UnixNano())
	fxRate := float64(micros) / 1e6
	if _, err := store.CreateCreditTransaction(ctx, userID, orderID, 100, fxRate); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompleteCreditTransaction(ctx, orderID, "pay_"+orderID); err != nil {
		t.Fatal(err)
	}
}

func setupDebitTestFixtures(t *testing.T, store *db.Store, fundMicros int64) (userID, workflowID, runID string) {
	t.Helper()
	ctx := context.Background()

	email := fmt.Sprintf("debit-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, fundMicros)

	wf, err := store.CreateWorkflow(ctx, "Debit Test WF", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	return user.ID, wf.ID, run.ID
}

func TestDebitCreditsSuccessDecrementsBalanceAndWritesLedger(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	userID, workflowID, runID := setupDebitTestFixtures(t, store, 100000) // 10 cents

	if err := store.DebitCredits(ctx, userID, 10000, "byok_flat_fee", workflowID, runID, "node1"); err != nil {
		t.Fatal(err)
	}

	balance, err := store.GetCreditBalance(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 90000 {
		t.Fatalf("want balance 90000 got %d", balance)
	}

	entries, err := store.ListDebitLedger(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 ledger entry got %d", len(entries))
	}
	e := entries[0]
	if e.UserID != userID || e.WorkflowID != workflowID || e.RunID != runID || e.NodeID != "node1" || e.Kind != "byok_flat_fee" || e.AmountUSDMicros != 10000 {
		t.Fatalf("unexpected ledger entry: %+v", e)
	}
}

func TestDebitCreditsInsufficientBalanceLeavesBalanceUnchanged(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	userID, workflowID, runID := setupDebitTestFixtures(t, store, 5000) // 0.5 cents

	err := store.DebitCredits(ctx, userID, 10000, "byok_flat_fee", workflowID, runID, "node1")
	if !errors.Is(err, db.ErrInsufficientCredits) {
		t.Fatalf("want ErrInsufficientCredits got %v", err)
	}

	balance, err := store.GetCreditBalance(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 5000 {
		t.Fatalf("balance must not change on a failed debit: want 5000 got %d", balance)
	}

	entries, err := store.ListDebitLedger(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 ledger entries on a failed debit, got %d", len(entries))
	}
}

func TestDebitCreditsConcurrentCallsNeverGoNegative(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	const fee = int64(10000)
	userID, workflowID, runID := setupDebitTestFixtures(t, store, fee*3) // exactly 3 fees' worth

	var wg sync.WaitGroup
	var succeeded int32
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := store.DebitCredits(ctx, userID, fee, "byok_flat_fee", workflowID, runID, fmt.Sprintf("node%d", i))
			if err == nil {
				atomic.AddInt32(&succeeded, 1)
			} else if !errors.Is(err, db.ErrInsufficientCredits) {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if succeeded != 3 {
		t.Fatalf("want exactly 3 successful debits out of 5 concurrent attempts, got %d", succeeded)
	}
	balance, err := store.GetCreditBalance(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance exactly 0 (never negative), got %d", balance)
	}
}
