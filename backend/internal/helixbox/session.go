package helixbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Session preflight: refusing to charge for a purchase that cannot land.
//
// # History, because it explains the shape of this file
//
// HelixBox originally settled payment before reading the request body, and its
// phone app had no way to learn that a session had been paid for from outside
// the app. Six purchases were lost to that on 2026-09-08 before the mechanism
// was understood. HelixBox then shipped `fix: unlock externally paid agent
// sessions` (2026-09-08) which closed almost all of it:
//
//   - a validation middleware now runs BEFORE the payment middleware, so a
//     missing code answers 400 and an unknown code answers 404 with no
//     settlement at all;
//   - GET /v2/session-status reports a code's real state;
//   - the app polls that endpoint every 2s and connects itself once the
//     session reads as paid, so it no longer has to be attached at the moment
//     of purchase.
//
// That last point is why this file does NOT require the app to be connected.
// An earlier version did, which was correct against the old server and would
// now block every legitimate purchase.
//
// # What is left to guard
//
// One trap survives, and it is ours to catch because HelixBox has no reason to
// treat it as an error:
//
//	session.paidUntil = Math.max(session.paidUntil, Date.now() + durationMs);
//
// Buying a plan SHORTER than the time already on a session is a no-op. The
// payment settles, the vendor keeps it, and paidUntil does not move — so a
// $1.75 hour bought on top of a live $3.50 week buys literally nothing. See
// StateAlreadyCovered.
//
// The missing-session check is kept as well. HelixBox now rejects those before
// charging, so it is no longer the difference between paying and not, but it
// still turns a bare 404 from a third party into a message that says what to do.

// SessionState is what a preflight found.
type SessionState int

const (
	// StateUnknown: the probe could not reach a verdict. Callers must let the
	// purchase proceed — see Ready's doc comment.
	StateUnknown SessionState = iota
	// StateMissing: HelixBox has no live session for this code.
	StateMissing
	// StateAlreadyCovered: the session is already paid past where this plan
	// would take it, so buying it would move nothing.
	StateAlreadyCovered
	// StateReady: the purchase will extend this session.
	StateReady
)

// Blocked reports whether this state means a purchase would be wasted.
// StateUnknown is deliberately NOT blocked.
func (s SessionState) Blocked() bool {
	return s == StateMissing || s == StateAlreadyCovered
}

// sessionStatus is HelixBox's GET /v2/session-status payload.
//
// Exists and Paid are POINTERS so an absent field is distinguishable from a
// false one. Decoding an unrelated 200 body into plain bools yields
// exists=false, which this file would otherwise read as "HelixBox says there is
// no such session" and block a purchase on the strength of a response that
// never mentioned it. HelixBox's own app client makes the same distinction
// (`typeof payload.paid !== 'boolean'` is an error there, not a false).
type sessionStatus struct {
	Code         string `json:"code"`
	Exists       *bool  `json:"exists"`
	Paid         *bool  `json:"paid"`
	PaidUntil    int64  `json:"paidUntil"`
	ExpiresAt    int64  `json:"expiresAt"`
	AppConnected bool   `json:"appConnected"`
	CLIConnected bool   `json:"cliConnected"`
}

// Message is what to tell someone about to spend money, in their terms. Each
// one names the thing they should do instead.
func (s SessionState) Message(paidUntil int64) string {
	switch s {
	case StateMissing:
		return "HelixBox does not recognise that pairing code. Run `npx helixbox-cli` again and use the session code it prints. Nothing was charged."
	case StateAlreadyCovered:
		until := time.UnixMilli(paidUntil).Local().Format("Mon 2 Jan, 15:04")
		return fmt.Sprintf("This session is already paid until %s, which is further ahead than this plan would reach — buying it would not add any time. Pick a longer plan, or come back nearer the end. Nothing was charged.", until)
	}
	return ""
}

// Ready probes a pairing code and reports whether a purchase can actually add
// time to it. paidUntil is echoed back for StateAlreadyCovered's message.
//
// Fails OPEN: any error, timeout, or unrecognised answer yields StateUnknown,
// which Blocked() reports as false. This is a guard against known, specific
// waste — not an authority on whether HelixBox will honour a payment. If they
// change this endpoint, the console must keep working rather than refuse every
// purchase because a check we added stopped getting the answer it expected.
func Ready(ctx context.Context, client *http.Client, code string, planDurationSeconds int64) (SessionState, int64) {
	return readyAt(ctx, client, "https://"+Host, code, planDurationSeconds)
}

// readyAt is Ready with the origin injectable, so the response handling can be
// tested against a stub instead of the live vendor. Probing the real endpoint
// from a test would be flaky and would depend on the state of somebody's
// actual paired session.
func readyAt(ctx context.Context, client *http.Client, baseURL, code string, planDurationSeconds int64) (SessionState, int64) {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	u := baseURL + "/v2/session-status?code=" + url.QueryEscape(code)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return StateUnknown, 0
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return StateUnknown, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return StateMissing, 0
	}
	if resp.StatusCode != http.StatusOK {
		return StateUnknown, 0
	}

	var st sessionStatus
	// Cap the read: this is a third-party response on a paid path, and a body
	// that never ends should time out rather than grow without limit.
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 64<<10)).Decode(&st); err != nil {
		return StateUnknown, 0
	}
	// A 200 that does not answer the question is not an answer. Only treat the
	// session as missing when HelixBox actually said so.
	if st.Exists == nil {
		return StateUnknown, 0
	}
	if !*st.Exists {
		// An explicit "no such session" settles it; whether it is paid is moot.
		return StateMissing, 0
	}
	// The session exists, so the decision now turns on paid/paidUntil. Without
	// a paid flag there is nothing to decide on — let the purchase through.
	if st.Paid == nil {
		return StateUnknown, 0
	}

	// The Math.max trap. Only block when the session is ALREADY past where this
	// plan would take it — equal or later means the purchase adds nothing.
	//
	// Uses HelixBox's clock via paidUntil against ours, which is fine at this
	// granularity: the plans are an hour and a week, and the comparison only
	// has to be right to within the skew between two servers.
	if *st.Paid && planDurationSeconds > 0 {
		wouldReach := time.Now().Add(time.Duration(planDurationSeconds) * time.Second).UnixMilli()
		if st.PaidUntil >= wouldReach {
			return StateAlreadyCovered, st.PaidUntil
		}
	}
	return StateReady, st.PaidUntil
}
