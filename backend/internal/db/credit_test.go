package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
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

	credited, applied, err := store.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_test_1")
	if err != nil {
		t.Fatal(err)
	}
	if credited != wantMicros {
		t.Fatalf("want %d got %d", wantMicros, credited)
	}
	if !applied {
		t.Fatal("want applied=true for a fresh completion")
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != wantMicros {
		t.Fatalf("want balance %d got %d", wantMicros, balance)
	}

	// Replay must not double-credit, and must report applied=false.
	credited2, applied2, err := store.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_test_1")
	if err != nil {
		t.Fatal(err)
	}
	if credited2 != wantMicros {
		t.Fatalf("replay: want %d got %d", wantMicros, credited2)
	}
	if applied2 {
		t.Fatal("want applied=false on replay")
	}
	balance2, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance2 != wantMicros {
		t.Fatalf("replay must not double-credit: want %d got %d", wantMicros, balance2)
	}
}

func TestRefundCreditTransactionFullRefundReversesBalance(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("credit-refund-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	orderID := fmt.Sprintf("order_refund_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, user.ID, orderID, 50000, 0.012); err != nil {
		t.Fatal(err)
	}
	wantMicros := int64(50000.0 / 100.0 * 0.012 * 1e6)
	if _, _, err := store.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_refund_test"); err != nil {
		t.Fatal(err)
	}

	reversed, applied, err := store.RefundCreditTransaction(ctx, orderID, 50000)
	if err != nil {
		t.Fatal(err)
	}
	if reversed != wantMicros {
		t.Fatalf("want reversed %d got %d", wantMicros, reversed)
	}
	if !applied {
		t.Fatal("want applied=true for a fresh refund")
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance 0 after full refund, got %d", balance)
	}

	// Replay of the same cumulative refund amount must not double-reverse.
	reversed2, applied2, err := store.RefundCreditTransaction(ctx, orderID, 50000)
	if err != nil {
		t.Fatal(err)
	}
	if reversed2 != 0 {
		t.Fatalf("want 0 on replay, got %d", reversed2)
	}
	if applied2 {
		t.Fatal("want applied=false on replay")
	}
	balance2, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance2 != 0 {
		t.Fatalf("replay must not double-reverse: want 0 got %d", balance2)
	}
}

// TestCompleteCreditTransactionCannotDoubleDipAfterRefund guards against replaying a
// completion after a refund: Razorpay signatures don't expire, so a captured verify
// payload (or a duplicate payment.captured webhook delivery) can arrive again after the
// order has already been fully refunded. Gating on status == "completed" alone would miss
// this, since RefundCreditTransaction moves status to "refunded" — completed_at is the
// guard that must hold regardless of what status becomes afterward.
func TestCompleteCreditTransactionCannotDoubleDipAfterRefund(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("credit-doubledip-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	orderID := fmt.Sprintf("order_doubledip_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, user.ID, orderID, 50000, 0.012); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_doubledip_test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RefundCreditTransaction(ctx, orderID, 50000); err != nil {
		t.Fatal(err)
	}

	balanceAfterRefund, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balanceAfterRefund != 0 {
		t.Fatalf("want balance 0 after refund, got %d", balanceAfterRefund)
	}

	// Replaying the same completion (e.g. a re-delivered webhook, or the signed verify
	// payload replayed by an attacker) must not re-credit — the user already got their
	// money back via the refund.
	_, applied, err := store.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_doubledip_test")
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("want applied=false — replaying completion after a refund must not re-credit")
	}

	balanceAfterReplay, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balanceAfterReplay != 0 {
		t.Fatalf("double-dip: want balance 0 after replayed completion, got %d", balanceAfterReplay)
	}
}

func TestRefundCreditTransactionPartialRefundReversesProportionally(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("credit-partial-refund-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	orderID := fmt.Sprintf("order_partial_refund_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, user.ID, orderID, 100000, 0.012); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_partial_refund_test"); err != nil {
		t.Fatal(err)
	}

	// Refund half (50000 of 100000 paise).
	wantReversed := int64(50000.0 / 100.0 * 0.012 * 1e6)
	reversed, applied, err := store.RefundCreditTransaction(ctx, orderID, 50000)
	if err != nil {
		t.Fatal(err)
	}
	if reversed != wantReversed {
		t.Fatalf("want reversed %d got %d", wantReversed, reversed)
	}
	if !applied {
		t.Fatal("want applied=true for a fresh partial refund")
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantBalance := int64(100000.0/100.0*0.012*1e6) - wantReversed
	if balance != wantBalance {
		t.Fatalf("want balance %d got %d", wantBalance, balance)
	}
}

func TestRefundCreditTransactionNeverCompletedSkipsBalanceReversal(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("credit-neverdone-refund-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	// Order created but never completed — no credit was ever granted.
	orderID := fmt.Sprintf("order_neverdone_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, user.ID, orderID, 50000, 0.012); err != nil {
		t.Fatal(err)
	}

	reversed, applied, err := store.RefundCreditTransaction(ctx, orderID, 50000)
	if err != nil {
		t.Fatal(err)
	}
	if reversed != 0 {
		t.Fatalf("want 0 reversed for a never-completed order, got %d", reversed)
	}
	if !applied {
		t.Fatal("want applied=true — this is still a new refund event, just with nothing to reverse")
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance untouched at 0, got %d", balance)
	}
}

func TestRefundCreditTransactionUnknownOrder(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	_, _, err := store.RefundCreditTransaction(ctx, "order_does_not_exist_xyz", 100)
	if !errors.Is(err, db.ErrCreditTransactionNotFound) {
		t.Fatalf("want ErrCreditTransactionNotFound, got %v", err)
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

	// A unique, test-only provider, for the same reason
	// TestExpireStalePendingTransactionsScopesToProvider uses one: these
	// sweeps are scoped only by provider, not by user or row, and every
	// package's tests share one database. Sweeping the real "cashfree"
	// expired other packages' in-flight pending rows and counted their
	// concurrently-created ones, so the exact-count assertions below raced
	// whatever else happened to be funding a user at that moment.
	sweepProvider := fmt.Sprintf("cashfree-expiretest-%d", time.Now().UnixNano())

	orderID := fmt.Sprintf("order_expire_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransactionForProvider(ctx, sweepProvider, user.ID, orderID, 10000, 0.012); err != nil {
		t.Fatal(err)
	}

	// Row is only a few milliseconds old — a 24h threshold must not touch it.
	n, err := store.ExpireStalePendingTransactions(ctx, sweepProvider, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 rows expired (too fresh), got %d", n)
	}

	// A zero threshold (cutoff = the database's own now) makes the row
	// qualify as stale without racing a fixed small duration.
	n2, err := store.ExpireStalePendingTransactions(ctx, sweepProvider, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 1 {
		t.Fatalf("want exactly 1 row expired, got %d", n2)
	}

	// Re-running must not re-touch rows that are no longer 'pending'.
	n3, err := store.ExpireStalePendingTransactions(ctx, sweepProvider, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n3 != 0 {
		t.Fatalf("want 0 rows on second sweep (already expired), got %d", n3)
	}
}

func TestExpireStalePendingTransactionsScopesToProvider(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// The shared test database accumulates pending rows across test runs, so this test
	// verifies its own rows by provider_order_id rather than asserting on global affected
	// row counts (which would be flaky against that pre-existing data).
	url := os.Getenv("TEST_DATABASE_URL")
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	email := fmt.Sprintf("credit-expire-scope-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	// Both providers are unique and test-only. The swept one must be unique because using
	// the real "nowpayments" would expire every other package's concurrent nowpayments
	// pending rows against the shared test DB, flaking those tests (e.g. the handlers
	// package's TestCreateCryptoInvoiceLeavesOrphanedPendingRowOnInvoiceFailure). The
	// control one must be unique so that no other test's cashfree-scoped sweep can expire
	// the row this test expects to still be pending — what is being asserted is provider
	// scoping, which holds for any two distinct providers.
	sweepProvider := fmt.Sprintf("nowpayments-scopetest-%d", time.Now().UnixNano())
	controlProvider := fmt.Sprintf("cashfree-scopetest-%d", time.Now().UnixNano())

	controlOrderID := fmt.Sprintf("order_expire_cashfree_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransactionForProvider(ctx, controlProvider, user.ID, controlOrderID, 10000, 0.012); err != nil {
		t.Fatal(err)
	}

	cryptoOrderID := fmt.Sprintf("order_expire_crypto_%d", time.Now().UnixNano())
	if _, err := store.CreateCryptoCreditTransaction(ctx, user.ID, sweepProvider, cryptoOrderID, 1999); err != nil {
		t.Fatal(err)
	}

	// A zero threshold (cutoff = now) reliably makes a row created moments ago qualify as
	// stale without a timing race against a fixed small duration like 1ms. Scoping to the
	// unique swept provider must only ever touch that provider's row.
	if _, err := store.ExpireStalePendingTransactions(ctx, sweepProvider, 0); err != nil {
		t.Fatal(err)
	}

	var cryptoStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM credit_ledger WHERE provider_order_id = $1 AND provider = $2`,
		cryptoOrderID, sweepProvider,
	).Scan(&cryptoStatus); err != nil {
		t.Fatal(err)
	}
	if cryptoStatus != "expired" {
		t.Fatalf("want swept-provider row expired, got status %q", cryptoStatus)
	}

	var controlStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM credit_ledger WHERE provider_order_id = $1 AND provider = $2`,
		controlOrderID, controlProvider,
	).Scan(&controlStatus); err != nil {
		t.Fatal(err)
	}
	if controlStatus != "pending" {
		t.Fatalf("want control-provider row untouched by a scoped sweep, got status %q", controlStatus)
	}
}

func TestGetCreditTransactionUserID(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("credit-owner-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	orderID := fmt.Sprintf("order_owner_test_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, user.ID, orderID, 50000, 0.012); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetCreditTransactionUserID(ctx, "cashfree", orderID)
	if err != nil {
		t.Fatal(err)
	}
	if got != user.ID {
		t.Fatalf("want %s got %s", user.ID, got)
	}
}

func TestGetCreditTransactionUserIDUnknownOrder(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	_, err := store.GetCreditTransactionUserID(ctx, "cashfree", "no-such-order")
	if err == nil {
		t.Fatal("want an error for an unknown order")
	}
}

func TestCheckAndMarkLowBalance(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("low-balance-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	fundUser(t, store, user.ID, 10_000_000) // $10
	threshold := int64(5_000_000)           // $5
	workflowID, runID := mustWorkflowAndRun(t, store, user.ID)

	// Above the threshold: no notification, nothing recorded.
	notify, _, err := store.CheckAndMarkLowBalance(ctx, user.ID, threshold)
	if err != nil {
		t.Fatal(err)
	}
	if notify {
		t.Fatal("want no notification while above the threshold")
	}

	// Cross below the threshold: notifies exactly once, quoting the balance
	// the decision was made on.
	if err := store.DebitCredits(ctx, user.ID, 6_000_000, "byok_flat_fee", workflowID, runID, "n1"); err != nil {
		t.Fatal(err)
	}
	notify, balance, err := store.CheckAndMarkLowBalance(ctx, user.ID, threshold)
	if err != nil {
		t.Fatal(err)
	}
	if !notify {
		t.Fatal("want a notification on the crossing")
	}
	if balance != 4_000_000 {
		t.Fatalf("want balance 4000000 at the crossing, got %d", balance)
	}

	// A second check while still low must not notify again.
	notify, _, err = store.CheckAndMarkLowBalance(ctx, user.ID, threshold)
	if err != nil {
		t.Fatal(err)
	}
	if notify {
		t.Fatal("want no repeat notification while still below the threshold")
	}

	// Recover back above the threshold, then dip below again: notifies once more.
	if err := store.ReleaseReservedCredits(ctx, user.ID, 6_000_000); err != nil {
		t.Fatal(err)
	}
	notify, _, err = store.CheckAndMarkLowBalance(ctx, user.ID, threshold)
	if err != nil {
		t.Fatal(err)
	}
	if notify {
		t.Fatal("recovering above the threshold should not itself notify")
	}

	if err := store.DebitCredits(ctx, user.ID, 6_000_000, "byok_flat_fee", workflowID, runID, "n2"); err != nil {
		t.Fatal(err)
	}
	notify, _, err = store.CheckAndMarkLowBalance(ctx, user.ID, threshold)
	if err != nil {
		t.Fatal(err)
	}
	if !notify {
		t.Fatal("want a fresh notification on the second crossing, after recovering in between")
	}
}

// mustWorkflowAndRun exists only so TestCheckAndMarkLowBalance can call
// DebitCredits, which requires a workflow_id/run_id to satisfy debit_ledger's
// foreign keys -- the test itself is about the users row, not these.
func mustWorkflowAndRun(t *testing.T, store *db.Store, userID string) (workflowID, runID string) {
	t.Helper()
	ctx := context.Background()
	wf, err := store.CreateWorkflow(ctx, "Low Balance Test WF", userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })
	run, err := store.CreateRun(ctx, wf.ID, "test", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	return wf.ID, run.ID
}

// A top-up that lifts the balance back over the threshold clears the
// low-balance marker, so the next drop below it is reported again. Only a
// finished run used to clear it.
func TestTopUpClearsTheLowBalanceMarker(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("low-balance-topup-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	// $0: below the threshold, so the first check marks it.
	if notify, _, err := store.CheckAndMarkLowBalance(ctx, user.ID, models.LowBalanceThresholdUSDMicros); err != nil || !notify {
		t.Fatalf("first low-balance check = %v, %v; want true, nil", notify, err)
	}

	orderID := fmt.Sprintf("order_low_balance_%d", time.Now().UnixNano())
	// 500 INR at 0.012 is $6, back over the $5 threshold.
	if _, err := store.CreateCreditTransaction(ctx, user.ID, orderID, 50000, 0.012); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompleteCreditTransaction(ctx, "cashfree", orderID, "pay_low_balance"); err != nil {
		t.Fatal(err)
	}

	// Judged against a higher bar, $6 is low again: with the marker cleared,
	// that crossing notifies.
	if notify, _, err := store.CheckAndMarkLowBalance(ctx, user.ID, 10_000_000); err != nil || !notify {
		t.Fatalf("check after top-up = %v, %v; want true, nil (marker cleared)", notify, err)
	}
}
