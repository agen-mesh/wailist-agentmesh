package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/db"
)

// newHistoryUser creates a throwaway user for history tests. Each test needs its
// own so cross-user isolation is actually observable. Takes the caller's store
// rather than opening its own — testStore registers a pool close per call.
func newHistoryUser(t *testing.T, store *db.Store, tag string) string {
	t.Helper()
	email := fmt.Sprintf("history-%s-%d@example.com", tag, time.Now().UnixNano())
	user, err := store.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

// A user who has never bought anything must get an empty, non-nil slice — the
// handler marshals this straight to JSON and the billing UI expects `[]`, not
// `null`.
func TestListCreditHistoryReturnsEmptySliceForNewUser(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	userID := newHistoryUser(t, store, "empty")

	history, err := store.ListCreditHistory(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if history == nil {
		t.Fatal("want a non-nil empty slice (marshals to []), got nil (marshals to null)")
	}
	if len(history) != 0 {
		t.Fatalf("want 0 rows for a user with no purchases, got %d", len(history))
	}
}

// The billing UI renders the list top-down as "most recent first", so ordering is
// a real behaviour, not an incidental detail of the query.
func TestListCreditHistoryOrdersMostRecentFirst(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	userID := newHistoryUser(t, store, "order")

	// Distinct amounts make each row identifiable regardless of generated IDs.
	amounts := []int64{10000, 20000, 30000}
	for i, paise := range amounts {
		orderID := fmt.Sprintf("order_hist_order_%d_%d", i, time.Now().UnixNano())
		if _, err := store.CreateCreditTransaction(ctx, userID, orderID, paise, 0.012); err != nil {
			t.Fatal(err)
		}
		// created_at is a timestamp; without a gap the ORDER BY has ties and the
		// assertion below would be testing insertion luck rather than ordering.
		time.Sleep(5 * time.Millisecond)
	}

	history, err := store.ListCreditHistory(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != len(amounts) {
		t.Fatalf("want %d rows got %d", len(amounts), len(history))
	}

	// Newest first: the last-inserted amount (30000) must lead.
	for i := range history {
		want := amounts[len(amounts)-1-i]
		if history[i].AmountINRPaise == nil {
			t.Fatalf("row %d: amount_inr_paise unexpectedly NULL", i)
		}
		if *history[i].AmountINRPaise != want {
			t.Fatalf("row %d: want amount %d got %d (ordering is not most-recent-first)",
				i, want, *history[i].AmountINRPaise)
		}
	}

	// Timestamps must be non-increasing, which is the property the UI relies on.
	for i := 1; i < len(history); i++ {
		if history[i].CreatedAt.After(history[i-1].CreatedAt) {
			t.Fatalf("row %d (%s) is newer than row %d (%s) — not sorted DESC",
				i, history[i].CreatedAt, i-1, history[i-1].CreatedAt)
		}
	}
}

// The query caps at 100 rows. A heavy user must not get an unbounded response.
func TestListCreditHistoryCapsAtOneHundredRows(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	userID := newHistoryUser(t, store, "limit")

	const total = 105
	for i := 0; i < total; i++ {
		orderID := fmt.Sprintf("order_hist_limit_%d_%d", i, time.Now().UnixNano())
		if _, err := store.CreateCreditTransaction(ctx, userID, orderID, int64(1000+i), 0.012); err != nil {
			t.Fatal(err)
		}
	}

	history, err := store.ListCreditHistory(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 100 {
		t.Fatalf("want the query capped at 100 rows, got %d (inserted %d)", len(history), total)
	}
}

// Every row returned must belong to the caller. This is the query's half of the
// authorization story — the handler supplies a JWT-derived userID, and this
// guarantees that filter actually excludes everyone else's ledger.
func TestListCreditHistoryIsScopedToOneUser(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	alice := newHistoryUser(t, store, "alice")
	bob := newHistoryUser(t, store, "bob")

	aliceOrder := fmt.Sprintf("order_hist_alice_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, alice, aliceOrder, 50000, 0.012); err != nil {
		t.Fatal(err)
	}
	bobOrder := fmt.Sprintf("order_hist_bob_%d", time.Now().UnixNano())
	if _, err := store.CreateCreditTransaction(ctx, bob, bobOrder, 70000, 0.012); err != nil {
		t.Fatal(err)
	}

	aliceHistory, err := store.ListCreditHistory(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceHistory) != 1 {
		t.Fatalf("want exactly alice's 1 row, got %d", len(aliceHistory))
	}
	for _, row := range aliceHistory {
		if row.UserID != alice {
			t.Fatalf("alice's history leaked a row owned by %s", row.UserID)
		}
		if row.ProviderOrderID == bobOrder {
			t.Fatal("alice's history contains bob's transaction")
		}
	}

	// And the reverse, so a query that ignored the filter entirely can't pass by
	// coincidence of row counts.
	bobHistory, err := store.ListCreditHistory(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobHistory) != 1 {
		t.Fatalf("want exactly bob's 1 row, got %d", len(bobHistory))
	}
	if bobHistory[0].ProviderOrderID != bobOrder {
		t.Fatalf("want bob's own order got %s", bobHistory[0].ProviderOrderID)
	}
}
