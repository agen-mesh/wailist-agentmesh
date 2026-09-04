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

	url := os.Getenv("TEST_DATABASE_URL")
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	email := fmt.Sprintf("credit-expire-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	// A unique, test-only provider keeps the affected-row counts below exact for this
	// test's own row. Sweeping the real "cashfree" provider counts whatever pending rows
	// other tests left behind in the shared test database, which is not something this
	// test can assert on.
	provider := fmt.Sprintf("cashfree-expiretest-%d", time.Now().UnixNano())

	orderID := fmt.Sprintf("order_expire_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransactionForProvider(ctx, provider, user.ID, orderID, 10000, 0.012); err != nil {
		t.Fatal(err)
	}

	// Row was just created — a 24h threshold must not touch it.
	n, err := store.ExpireStalePendingTransactions(ctx, provider, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 rows expired (too fresh), got %d", n)
	}

	// Age the row in the database instead of shrinking the threshold towards zero. The
	// sweep compares a Go-side cutoff (time.Now() minus the threshold) against a
	// created_at generated by Postgres NOW(), so a sub-second threshold races both the
	// insert-to-sweep gap and any skew between the two clocks — on a fast runner the row
	// is simply not yet old enough and nothing is expired. A row backdated 48h is
	// unambiguously past a 24h cutoff no matter how the two clocks line up.
	if _, err := pool.Exec(ctx, `
		UPDATE credit_ledger SET created_at = NOW() - INTERVAL '48 hours'
		WHERE provider = $1 AND provider_order_id = $2
	`, provider, orderID); err != nil {
		t.Fatal(err)
	}

	n2, err := store.ExpireStalePendingTransactions(ctx, provider, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 1 {
		t.Fatalf("want exactly 1 row expired, got %d", n2)
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM credit_ledger WHERE provider = $1 AND provider_order_id = $2`,
		provider, orderID,
	).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "expired" {
		t.Fatalf("want stale row expired, got status %q", status)
	}

	// Re-running must not re-touch rows that are no longer 'pending'.
	n3, err := store.ExpireStalePendingTransactions(ctx, provider, 24*time.Hour)
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

	// Age both rows well past the sweep threshold in the database. A Go-side cutoff of
	// "now" (a zero threshold) is compared against a created_at generated by Postgres
	// NOW(), so it depends on how the two clocks line up; backdating 48h and sweeping at
	// 24h leaves a day of margin and makes staleness unambiguous. Both rows are aged so
	// that the control row staying pending proves provider scoping rather than merely
	// proving it was too fresh to expire.
	if _, err := pool.Exec(ctx, `
		UPDATE credit_ledger SET created_at = NOW() - INTERVAL '48 hours'
		WHERE provider_order_id = ANY($1)
	`, []string{controlOrderID, cryptoOrderID}); err != nil {
		t.Fatal(err)
	}

	// Scoping to the unique swept provider must only ever touch that provider's row.
	if _, err := store.ExpireStalePendingTransactions(ctx, sweepProvider, 24*time.Hour); err != nil {
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
