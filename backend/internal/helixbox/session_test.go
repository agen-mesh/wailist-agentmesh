package helixbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const hour = 3600
const week = 604800

func boolp(b bool) *bool { return &b }

// stubStatus answers GET /v2/session-status with a fixed payload.
func stubStatus(t *testing.T, status int, body any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session-status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("code") == "" {
			t.Error("probe sent no code")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestReadyBlocksAPurchaseThatWouldAddNoTime is the reason this preflight still
// exists now that HelixBox validates codes before charging.
//
// Their redeem does `paidUntil = Math.max(paidUntil, now + durationMs)`, so a
// plan that ends earlier than the time already on the session is a no-op that
// still settles: $1.75 for nothing. Nobody else in the stack will catch it —
// HelixBox has no reason to call it an error.
func TestReadyBlocksAPurchaseThatWouldAddNoTime(t *testing.T) {
	// A week already paid; buying an hour cannot move paidUntil.
	paidUntil := time.Now().Add(6 * 24 * time.Hour).UnixMilli()
	srv := stubStatus(t, http.StatusOK, sessionStatus{
		Code: "AfXavNVgBJ", Exists: boolp(true), Paid: boolp(true), PaidUntil: paidUntil,
	})

	state, got := readyAt(context.Background(), srv.Client(), srv.URL, "AfXavNVgBJ", hour)
	if state != StateAlreadyCovered {
		t.Fatalf("state = %v, want StateAlreadyCovered", state)
	}
	if !state.Blocked() {
		t.Error("a purchase that adds no time must be blocked")
	}
	if got != paidUntil {
		t.Errorf("paidUntil = %d, want %d — the message needs it to say until when", got, paidUntil)
	}

	msg := state.Message(got)
	if !strings.Contains(msg, "Nothing was charged") {
		t.Errorf("message does not say the money is safe: %q", msg)
	}
	// The date is the whole point: "already paid" without saying until when
	// leaves the user no way to decide what to do instead.
	if !strings.Contains(msg, time.UnixMilli(paidUntil).Local().Format("Mon 2 Jan")) {
		t.Errorf("message does not say when the session runs out: %q", msg)
	}
}

// The mirror case: a longer plan on top of a shorter remaining balance really
// does extend it, and must NOT be blocked.
func TestReadyAllowsAPurchaseThatExtendsTheSession(t *testing.T) {
	// Half an hour left; buying a week reaches far past it.
	srv := stubStatus(t, http.StatusOK, sessionStatus{
		Code: "AfXavNVgBJ", Exists: boolp(true), Paid: boolp(true),
		PaidUntil: time.Now().Add(30 * time.Minute).UnixMilli(),
	})
	state, _ := readyAt(context.Background(), srv.Client(), srv.URL, "AfXavNVgBJ", week)
	if state != StateReady {
		t.Fatalf("state = %v, want StateReady", state)
	}
	if state.Blocked() {
		t.Error("a purchase that genuinely extends the session was blocked")
	}
}

// An unpaid session is the ordinary first purchase and must always be allowed,
// whatever paidUntil happens to hold.
func TestReadyAllowsTheFirstPurchaseOnAnUnpaidSession(t *testing.T) {
	for _, st := range []sessionStatus{
		{Code: "c", Exists: boolp(true), Paid: boolp(false), PaidUntil: 0},
		{Code: "c", Exists: boolp(true), Paid: boolp(false), PaidUntil: time.Now().Add(-time.Hour).UnixMilli()},
		{Code: "c", Exists: boolp(true), Paid: boolp(false), CLIConnected: true},
	} {
		srv := stubStatus(t, http.StatusOK, st)
		if state, _ := readyAt(context.Background(), srv.Client(), srv.URL, "c", hour); state.Blocked() {
			t.Errorf("%+v was blocked; an unpaid session is the normal case", st)
		}
	}
}

// TestReadyDoesNotRequireEitherSideToBeConnected pins a deliberate reversal.
//
// An earlier version blocked when appConnected was false, which was correct
// against the OLD server: the app had to be attached at the instant of payment
// or the unlock never fired. Since HelixBox's 2026-09-08 fix the app polls
// /v2/session-status every 2s and connects itself once a session reads as paid,
// so requiring it now would block every legitimate purchase.
func TestReadyDoesNotRequireEitherSideToBeConnected(t *testing.T) {
	srv := stubStatus(t, http.StatusOK, sessionStatus{
		Code: "c", Exists: boolp(true), Paid: boolp(false),
		AppConnected: false, CLIConnected: false,
	})
	if state, _ := readyAt(context.Background(), srv.Client(), srv.URL, "c", hour); state.Blocked() {
		t.Fatal("blocked because nothing was connected; the app now attaches itself after payment")
	}
}

// A code HelixBox does not know cannot be paid into. They answer 404 before
// charging now, so this is about giving a better message than a bare 404.
func TestReadyBlocksACodeHelixboxDoesNotKnow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   any
	}{
		{"404 from the endpoint", http.StatusNotFound, map[string]any{"exists": false, "paid": false}},
		{"200 but exists:false", http.StatusOK, sessionStatus{Code: "c", Exists: boolp(false)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stubStatus(t, tc.status, tc.body)
			state, _ := readyAt(context.Background(), srv.Client(), srv.URL, "c", hour)
			if state != StateMissing {
				t.Fatalf("state = %v, want StateMissing", state)
			}
			msg := state.Message(0)
			if !strings.Contains(msg, "helixbox-cli") {
				t.Errorf("message does not say how to get a working code: %q", msg)
			}
			if !strings.Contains(msg, "Nothing was charged") {
				t.Errorf("message does not say the money is safe: %q", msg)
			}
		})
	}
}

// TestReadyFailsOpenOnAnythingItCannotInterpret is a deliberate design choice.
//
// This is a guard against two specific kinds of waste, not an authority on
// whether HelixBox will honour a payment. If they change the endpoint, the
// console must keep working rather than refuse every purchase because a check
// we added stopped getting the answer it expected.
func TestReadyFailsOpenOnAnythingItCannotInterpret(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   any
	}{
		{"server error", http.StatusInternalServerError, nil},
		{"rate limited", http.StatusTooManyRequests, nil},
		{"gateway down", http.StatusBadGateway, nil},
		{"unauthorized", http.StatusUnauthorized, nil},
		// A 200 that never mentions exists/paid: decoding it into plain
		// bools would yield exists=false and block a purchase on the
		// strength of a body that answered a different question.
		{"body is not the shape we expect", http.StatusOK, map[string]any{"whatever": true}},
		{"exists present but paid missing", http.StatusOK, map[string]any{"exists": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stubStatus(t, tc.status, tc.body)
			if state, _ := readyAt(context.Background(), srv.Client(), srv.URL, "c", hour); state.Blocked() {
				t.Errorf("%s blocked the purchase; an uninterpretable answer must fail open", tc.name)
			}
		})
	}

	// Malformed JSON on a 200.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not json"))
	}))
	defer bad.Close()
	if state, _ := readyAt(context.Background(), bad.Client(), bad.URL, "c", hour); state.Blocked() {
		t.Error("malformed JSON blocked the purchase")
	}

	// Unreachable manager.
	gone := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := gone.URL
	gone.Close()
	if state, _ := readyAt(context.Background(), http.DefaultClient, addr, "c", hour); state.Blocked() {
		t.Error("an unreachable manager blocked the purchase")
	}

	// Cancelled context.
	srv := stubStatus(t, http.StatusOK, sessionStatus{Code: "c", Exists: boolp(true)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if state, _ := readyAt(ctx, srv.Client(), srv.URL, "c", hour); state.Blocked() {
		t.Error("a cancelled probe blocked the purchase")
	}
}

// Only blocking states carry a message; there is nothing to tell a user whose
// purchase is about to go through.
func TestOnlyBlockingStatesCarryAMessage(t *testing.T) {
	for _, s := range []SessionState{StateUnknown, StateReady} {
		if s.Blocked() {
			t.Errorf("%v must not block", s)
		}
		if s.Message(time.Now().UnixMilli()) != "" {
			t.Errorf("%v carries a message but nothing is wrong", s)
		}
	}
}

// The probe must stay a plain read. Anything else on a paid path risks side
// effects on the vendor's session state.
func TestProbeIsAPlainReadOnlyGet(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			t.Error("the probe sent upgrade headers")
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(sessionStatus{Code: "c", Exists: boolp(true)})
	}))
	defer srv.Close()

	readyAt(context.Background(), srv.Client(), srv.URL, "c", hour)
	if method != http.MethodGet {
		t.Errorf("probe used %s, want GET", method)
	}
	if path != "/v2/session-status" {
		t.Errorf("probe hit %s, want /v2/session-status", path)
	}
}
