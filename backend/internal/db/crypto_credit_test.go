package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCryptoCreditTransactionLifecycle(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("crypto-credit-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	orderID := fmt.Sprintf("order_crypto_%d", time.Now().UnixNano())
	txn, err := store.CreateCryptoCreditTransaction(ctx, user.ID, "nowpayments", orderID, 1999)
	if err != nil {
		t.Fatal(err)
	}
	if txn.Status != "pending" {
		t.Fatalf("want pending got %s", txn.Status)
	}
	if txn.Provider != "nowpayments" {
		t.Fatalf("want provider nowpayments got %s", txn.Provider)
	}
	wantMicros := int64(1999 * 10_000)
	if txn.CreditUSDMicros != wantMicros {
		t.Fatalf("want %d got %d", wantMicros, txn.CreditUSDMicros)
	}

	credited, applied, err := store.CompleteCreditTransaction(ctx, "nowpayments", orderID, "6084744717")
	if err != nil {
		t.Fatal(err)
	}
	if credited != wantMicros || !applied {
		t.Fatalf("want credited=%d applied=true, got credited=%d applied=%v", wantMicros, credited, applied)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != wantMicros {
		t.Fatalf("want balance %d got %d", wantMicros, balance)
	}
}

func TestCryptoAndRazorpayOrderIDsDoNotCollideAcrossProviders(t *testing.T) {
	// Two different providers are free to hand out the same order_id string — the
	// (provider, provider_order_id) pair is the real uniqueness key, not order_id alone.
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("collision-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	sharedOrderID := fmt.Sprintf("shared_order_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, user.ID, sharedOrderID, 50000, 0.012); err != nil {
		t.Fatalf("razorpay create: %v", err)
	}
	if _, err := store.CreateCryptoCreditTransaction(ctx, user.ID, "nowpayments", sharedOrderID, 1999); err != nil {
		t.Fatalf("crypto create with same order_id but different provider should succeed: %v", err)
	}
}

func TestMarkCreditTransactionStatusDoesNotTouchBalance(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	email := fmt.Sprintf("mark-status-test-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}

	orderID := fmt.Sprintf("order_failed_%d", time.Now().UnixNano())
	if _, err := store.CreateCryptoCreditTransaction(ctx, user.ID, "nowpayments", orderID, 1999); err != nil {
		t.Fatal(err)
	}

	if err := store.MarkCreditTransactionStatus(ctx, "nowpayments", orderID, "failed"); err != nil {
		t.Fatal(err)
	}

	balance, err := store.GetCreditBalance(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatalf("want balance untouched at 0, got %d", balance)
	}

	// A subsequent finished IPN (e.g. a delayed/out-of-order retry) must not resurrect
	// and credit a row already marked failed.
	if _, applied, err := store.CompleteCreditTransaction(ctx, "nowpayments", orderID, "pay_1"); err != nil {
		t.Fatal(err)
	} else if applied {
		t.Fatal("want a failed row to stay non-completable")
	}
}
