// Package helixbox holds the specification for HelixBox's x402 endpoints:
// their URLs, their live-probed payment quotes, and what each one sells.
//
// It exists as its own leaf package for the same reason internal/prism does —
// two unrelated callers need the same facts and must never disagree:
//
//   - internal/bazaar builds HelixBox's curated catalog entries from
//     Endpoints(), so the price on a Bazaar card is literally the same value as
//   - internal/api/handlers, which builds and pays the real request from the
//     same table.
//
// # On the quotes in this file
//
// Every PayTo and AmountMicros here was transcribed from a live 402 challenge
// probed on 2026-09-08, not from documentation and never from a guess:
//
//	curl -X POST -D - -d '{}' -H 'Content-Type: application/json' \
//	  https://helixbox-manager.onrender.com/<path>
//	# -> 402, payment-required: <base64 of the challenge JSON>
//
// All three agreed with HelixBox's written spec exactly. Worth recording that
// the spec document's own base64 blob for /v2/x402/premium/week is corrupt —
// it decodes to a mangled payTo — while the decoded JSON printed beside it is
// correct and matches the live probe. That is precisely why these values are
// probed rather than transcribed: a wrong PayTo does not fail loudly, it pays
// a stranger.
//
// Re-probe before editing any amount or address in this file.
//
// # The challenge's declared input is NOT the endpoint's real input
//
// Read this before trusting any x402 challenge's bazaar extension again.
//
// All three challenges declare their request body as `{}` with an empty
// JSON-Schema property set and additionalProperties:false. That is not what
// the endpoints accept. Every one of them requires a CLI pairing code:
//
//	POST /v2/x402/cli/hour  {"code":"AfXavNVgBJ"}
//
// Sending `{}` costs a full payment and then returns 400 "CLI pairing code is
// required" — the vendor keeps the money, because settlement happens before
// the handler ever inspects the body. That is not a hypothetical: it happened
// twice on 2026-09-08, at $1.75 a time, building this package.
//
// The cause is visible in HelixBox's own source (manager/src/x402-app.ts):
// declareDiscoveryExtension() is called with an `output` schema only, so the
// library emitted a DEFAULT empty input schema. An absent declaration and a
// declaration of "takes nothing" are indistinguishable on the wire, and the
// expensive one is assuming the second.
//
// So the request shape below is transcribed from their handler, not from the
// challenge:
//
//	const body = await c.req.json<{ code?: string }>().catch(() => ({}));
//	const code = (body.code || "").trim();
//	if (!code) return c.json({ error: "CLI pairing code is required" }, 400);
//	return c.json(await redeemSession(code, Date.now() + durationMs));
//
// # What a purchase actually does
//
// It does NOT mint a session token, whatever the spec document says. The user
// already has a session: they ran `npx helixbox-cli` on their machine, which
// printed a QR code and a 10-character pairing code. Paying extends that
// session's paid-until time. The reply echoes the code back with the new
// expiry — {"code":"AfXavNVgBJ","expiresAt":1757320000000} — where expiresAt
// is absolute epoch MILLISECONDS, not a duration.
//
// # The hazards, and which ones are still live
//
// As originally shipped, this endpoint pair could take money in three ways that
// produced nothing. HelixBox closed two of them on 2026-09-08 in
// `fix: unlock externally paid agent sessions`:
//
//   - FIXED — a missing or unknown code settled first and failed second. A
//     validation middleware now runs ahead of the payment middleware and
//     answers 400/404 with nothing charged.
//   - FIXED — the phone app could not learn a session had been paid for from
//     outside itself, so an out-of-band purchase never unlocked anything. The
//     app now polls GET /v2/session-status every 2s and connects itself.
//   - STILL LIVE — paidUntil moves by Math.max, so buying a plan that ends
//     sooner than the time already on a session is a no-op that still settles.
//     Nothing on their side treats it as an error, because from their side it
//     is not one. See helixbox.Ready, which is what stops it here.
package helixbox

import "net/http"

// Host is HelixBox's origin. Every endpoint below must live on it; a spec
// pointing anywhere else is a bug, and TestEndpointsAreWellFormed enforces it.
const Host = "helixbox-manager.onrender.com"

// Provider is the display name, spelled the way HelixBox spells it.
const Provider = "HelixBox"

// ConsoleKey marks these endpoints as backed by the HelixBox console page
// rather than by a canvas node. It is a provider key, never a URL: the frontend
// maps it to a route it already owns, so no catalog data can ever redirect a
// user.
const ConsoleKey = "helixbox"

// Network is the CAIP-2 id every HelixBox challenge declares (Algorand
// mainnet), and AssetID is the USDC ASA every one of them prices in.
const (
	Network = "algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8="
	AssetID = "31566704"
)

// PayTo is the single settlement address all three endpoints share (verified
// identical across all three probes on 2026-09-08).
const PayTo = "AQYWNHO6QWB4AB4SHIVMNZL2QN2ZQIYYO3Z27DJCUOILZ43YGGZUIPAURY"

// redeemBody is the request body all three endpoints take: the CLI pairing
// code, and nothing else. Transcribed from HelixBox's handler — see the
// package comment on why the challenge's own declared schema is not usable
// here. {{param:code}} is expanded and JSON-escaped by
// engine/nodes.expandBodyTemplate.
const redeemBody = `{"code":"{{param:code}}"}`

// PairingCodeLength is how many characters GET /v2/qr hands out (probed
// 2026-09-08: "AfXavNVgBJ", "syPYtHEDcs" — 10 mixed-case alphanumerics).
//
// Used only to spot an obviously-wrong paste BEFORE paying. It is deliberately
// not enforced as a hard rule: HelixBox accepts any non-empty string, and
// refusing to spend a user's own money on a code their CLI really printed —
// because we guessed the format wrong — would be worse than the warning.
const PairingCodeLength = 10

// Field kinds, mirroring prism.Field so the two consoles render from the same
// shape. HelixBox needs only text today.
const (
	FieldText = "text"
)

// Field is one input on an endpoint's console form.
type Field struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
	Description string `json:"description,omitempty"`
}

// pairingFields is the one input every plan takes. Shared by all three so a
// reworded hint cannot drift between them.
func pairingFields() []Field {
	return []Field{
		{
			Name:        "code",
			Label:       "Pairing code",
			Kind:        FieldText,
			Required:    true,
			Placeholder: "AfXavNVgBJ",
			Description: "Run `npx helixbox-cli` on the machine you want to reach. It prints a QR code with the session code underneath — that code goes here. Scan the QR with the HelixBox app and it picks up the time you buy within a couple of seconds.",
		},
	}
}

// Verification states for Endpoint.Verified, strongest first. Mirrors
// prism.Verified* deliberately: same meaning, same wording in the UI.
//
// These describe how much is known about an endpoint's REQUEST shape, which is
// the only thing that can cause a paid-then-rejected call. They are not a
// quality rating.
const (
	// VerifiedLive: a real paid call to this exact endpoint succeeded and
	// returned a usable session.
	VerifiedLive = "live"
	// VerifiedSource: the shape is transcribed from the vendor's own published
	// handler code, which is stronger than their spec document and very much
	// stronger than their 402 challenge — both of which were wrong here.
	//
	// This state exists because "documented" was not honest enough. The
	// previous version of this file called all three endpoints documented on
	// the strength of the challenge's declared input schema, and that schema
	// was a library default meaning "nobody declared one". Two paid calls were
	// lost to the difference, so the provenance is now named precisely rather
	// than flattened into one confident word.
	VerifiedSource = "source"
)

// Endpoint is one payable HelixBox plan.
//
// "Plan", not "task": Prism's endpoints answer a question, HelixBox's sell a
// window of access. That difference drives the whole console — there is no
// form to fill in, and the thing you get back is a credential you have to keep.
type Endpoint struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Description is HelixBox's own, taken verbatim from the probed
	// challenge's resource.description.
	Description string `json:"description"`
	// Blurb is AgentMesh's plain-language line about who the plan is for.
	// Separate from Description because the vendor's sentence describes the
	// product and this one has to help someone choose between three of them.
	Blurb  string `json:"blurb"`
	Path   string `json:"path"`
	Method string `json:"method"`
	// AmountMicros is the vendor's price in atomic USDC (6 decimals). It is
	// NOT what the caller pays: AgentMesh's flat platform markup
	// (models.X402PlatformFeeUSDMicros) is added on top by the relay, and the
	// console surfaces the sum.
	AmountMicros int64 `json:"amountMicros"`
	// DurationSeconds is how long the purchased session lasts, and must match
	// the expiresIn the endpoint returns. Carried here so the console can
	// price and label a plan BEFORE anything is bought — the response's own
	// expiresIn only arrives after the money has moved.
	DurationSeconds int64 `json:"durationSeconds"`
	// AccessLevel is the tier the session token carries ("cli", "agent",
	// "premium"), matching the endpoint's accessLevel in its response.
	AccessLevel string `json:"accessLevel"`
	// BodyTemplate is sent verbatim as the JSON body, with {{param:x}}
	// expanded from the caller's fields. Excluded from JSON: the frontend
	// posts field VALUES and has no business seeing — still less influencing —
	// the shape of the body that gets paid for.
	BodyTemplate string `json:"-"`
	// Fields are the console form's inputs.
	Fields   []Field `json:"fields"`
	Verified string  `json:"verified"`
}

// URL is the endpoint's full https URL.
func (e Endpoint) URL() string { return "https://" + Host + e.Path }

// Endpoints returns all three HelixBox plans, cheapest first. The slice is
// rebuilt per call so no caller can mutate a shared table.
func Endpoints() []Endpoint {
	return []Endpoint{
		{
			ID:              "cli-hour",
			Title:           "CLI hour",
			Description:     "One hour of HelixBox agent session access.",
			Blurb:           "Reach your own machine's terminal from your phone for an hour. Files, logs and Git, wherever you are.",
			Path:            "/v2/x402/cli/hour",
			Method:          http.MethodPost,
			AmountMicros:    250_000,
			DurationSeconds: 3600,
			AccessLevel:     "cli",
			BodyTemplate:    redeemBody,
			Fields:          pairingFields(),
			Verified:        VerifiedSource,
		},
		{
			ID:          "agent-session-hour",
			Title:       "Agent hour",
			Description: "One hour of HelixBox AI agent session and remote CLI access.",
			Blurb:       "Everything in the CLI hour, plus an AI agent that can run the commands for you. Same price, same hour.",
			Path:        "/v2/x402/agent-session-1hour",
			Method:      http.MethodPost,
			// Same price as the CLI hour by HelixBox's own pricing (both
			// probed at 250000 on 2026-09-08) — not a copy-paste slip. The
			// console says so, because two identical prices side by side read
			// like a bug otherwise.
			AmountMicros:    250_000,
			DurationSeconds: 3600,
			AccessLevel:     "agent",
			BodyTemplate:    redeemBody,
			Fields:          pairingFields(),
			Verified:        VerifiedSource,
		},
		{
			ID:              "premium-week",
			Title:           "Premium week",
			Description:     "Seven days of HelixBox premium agent session access.",
			Blurb:           "A full week of premium access. Works out cheaper than four separate hours if you are using it across a few days.",
			Path:            "/v2/x402/premium/week",
			Method:          http.MethodPost,
			AmountMicros:    2_000_000,
			DurationSeconds: 604800,
			AccessLevel:     "premium",
			BodyTemplate:    redeemBody,
			Fields:          pairingFields(),
			Verified:        VerifiedSource,
		},
	}
}

// Lookup finds an endpoint by id. The second return is false for an unknown
// id, which callers must treat as a client error and never as a passthrough:
// the URL a call is made against comes from this table, never from a request
// body, so /helixbox/run cannot be turned into an open x402 proxy that spends
// a user's credit against an arbitrary host.
func Lookup(id string) (Endpoint, bool) {
	for _, e := range Endpoints() {
		if e.ID == id {
			return e, true
		}
	}
	return Endpoint{}, false
}
