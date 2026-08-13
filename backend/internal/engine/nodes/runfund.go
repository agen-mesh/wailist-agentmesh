package nodes

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/agentmesh/backend/internal/x402"
)

// runFundingPublicPath is this route as reached through our own branded
// domain's /api proxy (frontend/next.config.ts rewrites /api/:path* to the
// backend). Used for both the declared resource URL and the bazaar
// routeTemplate, which must agree -- see x402relay.go's relayPublicPath.
const runFundingPublicPath = "/api/x402/relay/run-funding"

// platformFeePublicPath is SettlePlatformFee's resource identity, same
// rationale as runFundingPublicPath -- a real, non-root, genuinely
// 402-answering path under our own branded origin, distinct from
// runFundingPublicPath so the two settlement kinds attribute separately in
// any per-resource reporting even though both share one payTo.
const platformFeePublicPath = "/api/x402/relay/platform-fee"

// RunPreFundConfig carries what's needed to settle a single lump-sum inbound
// x402 payment (Wallet 1 -> Wallet 2) before an agent node's tool-calling
// loop starts. Distinct from Wallet2PayConfig (Task 3), which drives the
// per-call OUTBOUND leg — this only ever settles the INBOUND leg, once per
// run.
type RunPreFundConfig struct {
	USDCSigner               USDCGroupSigner
	PlatformSpendEncMnemonic string
	Facilitator              *x402.FacilitatorClient
	PlatformWalletAddress    string
	RelayNetwork             string
	RelayFeePayer            string
	ExpectedAssetID          uint64
	// FrontendURL is our own branded origin (FRONTEND_URL), combined below
	// with the /api/... proxy path that frontend/next.config.ts already
	// rewrites to this backend. Same identity x402relay.go declares -- see
	// its relayPublicPath doc comment for why the origin has to be this one
	// and the path has to be a real, non-root, genuinely-402-answering one.
	FrontendURL string
}

// FundRunReserve settles one real GoPlausible payment for amountUSDMicros
// from the platform's Wallet 1 spend wallet into Wallet 2
// (cfg.PlatformWalletAddress) — same payTo as every per-call relay
// settlement, so leaderboard attribution (keyed on payTo, not resource) is
// unaffected. Resource points at the real run-funding route as reached
// through our own branded domain's /api proxy, rather than an opaque
// identifier string or a bare backend host.
// amountUSDMicros <= 0 is a no-op (an agent with no attached tool402 nodes,
// or all-legacy-dialect ones, needs no pre-fund at all).
func FundRunReserve(ctx context.Context, cfg RunPreFundConfig, runID string, amountUSDMicros int64) (string, error) {
	return selfSettleWallet1ToWallet2(ctx, cfg, runFundingPublicPath,
		"AgentMesh workflow run funding — pre-settled pool for this run's downstream x402 tool calls",
		"run pre-fund", amountUSDMicros)
}

// SettlePlatformFee settles the platform's own flat per-call markup
// (models.X402PlatformFeeUSDMicros) as one more real, direct Wallet 1 ->
// Wallet 2 payment, same mechanism as FundRunReserve but for the fee alone
// rather than a whole run's estimated vendor spend.
//
// Exists so the fee actually lands in cfg.PlatformWalletAddress on-chain
// instead of being a pure internal credit-ledger bookkeeping entry with no
// backing asset movement -- before this, the per-call relay path's
// executeTool402V2Relay committed the fee against the caller's AgentMesh
// credit balance and stopped there: real USDC only ever moved for the
// vendor's own real ask (see that function's `total := amount + fee` split,
// where only `amount` is ever signed/settled). Called right alongside that
// vendor-cost settlement, not baked into it, because the vendor leg's price
// is set by the vendor's own external 402 challenge -- inflating it by our
// own markup would overpay whatever the vendor actually quoted and break
// protocol compatibility for any other caller of our public relay endpoint
// (x402relay.go's X402Relay is unauthenticated and bazaar-cataloged, used by
// more than just this platform's own engine).
func SettlePlatformFee(ctx context.Context, cfg RunPreFundConfig, amountUSDMicros int64) (string, error) {
	return selfSettleWallet1ToWallet2(ctx, cfg, platformFeePublicPath,
		"AgentMesh platform fee — flat per-call markup, settled from the platform spend wallet to the platform revenue wallet",
		"platform fee settle", amountUSDMicros)
}

// selfSettleWallet1ToWallet2 signs, verifies, and settles one real GoPlausible
// payment for amountUSDMicros from the platform's own Wallet 1 spend wallet
// into Wallet 2 (cfg.PlatformWalletAddress) -- no external target involved,
// the platform is both payer and resource server. publicPath/description
// give the settlement its own distinct, genuinely-402-answering resource
// identity (see runFundingPublicPath's doc comment for why that has to be a
// real path under our own branded origin, not an opaque identifier).
// amountUSDMicros <= 0 is a no-op.
//
// Retries up to selfSettleMaxAttempts times on any failure EXCEPT
// ErrSettlementIndeterminate: a signing error, a verify rejection, or a
// definitive (received) settle failure all mean nothing was broadcast or
// confirmed, so a retry -- with a fresh SignUSDCPaymentGroup call, and
// therefore a fresh uniqueNote nonce and SuggestedParams -- is safe and
// cannot double-pay. An indeterminate settle response (the request may
// have already been broadcast and confirmed, we just never heard back)
// stops retrying immediately instead, exactly as before this change --
// resubmitting there risks paying twice. This is what actually closes the
// gap: before wallet/algorand.go's uniqueNote fix, a same-round retry of
// the flat-amount platform fee would have produced the exact same
// collision it was retrying to escape; now every attempt is guaranteed
// distinct regardless of amount or timing.
//
// ctx.Err() is checked before every attempt, including the first: without
// this, a caller whose context is already canceled/expired when this is
// entered would still burn up to selfSettleMaxAttempts real sign+verify
// round trips before giving up, and each of those failures would (before
// this check existed) get folded into lastErr and returned looking like a
// genuine payment failure rather than "the caller gave up before we ever
// really tried." Callers using SelfSettleRetryBudget for their own
// context.WithTimeout (SettlePlatformFee's and FundRunReserve's call
// sites both do) size it for selfSettleMaxAttempts full attempts, so a
// slow-but-definitive failure on an early attempt doesn't starve a later
// retry of its own fair shot -- this check is what makes that budget
// actually save a caller work once it's genuinely exhausted, rather than
// racing to fit one more doomed attempt in and misclassifying its
// resulting deadline-exceeded error as ErrSettlementIndeterminate.
func selfSettleWallet1ToWallet2(ctx context.Context, cfg RunPreFundConfig, publicPath, description, errPrefix string, amountUSDMicros int64) (string, error) {
	if amountUSDMicros <= 0 {
		return "", nil
	}

	var lastErr error
	for attempt := 1; attempt <= selfSettleMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return "", fmt.Errorf("%s: giving up after %d attempt(s), context done: %w (last attempt's error: %v)", errPrefix, attempt-1, err, lastErr)
			}
			return "", fmt.Errorf("%s: context done before any attempt: %w", errPrefix, err)
		}
		txID, err := attemptSelfSettle(ctx, cfg, publicPath, description, errPrefix, amountUSDMicros)
		if err == nil {
			return txID, nil
		}
		if errors.Is(err, ErrSettlementIndeterminate) {
			return "", err
		}
		lastErr = err
	}
	return "", lastErr
}

// selfSettleMaxAttempts bounds selfSettleWallet1ToWallet2's retry loop. Not
// unbounded: a real, persistent misconfiguration (bad mnemonic, algod down,
// facilitator down) should fail loudly and quickly, not loop for minutes.
const selfSettleMaxAttempts = 3

// SelfSettleAttemptBudget is the worst-case time ONE attemptSelfSettle call
// (sign + verify + settle) can take: up to 20s each for the facilitator's
// Verify and Settle calls (x402.FacilitatorClient's own http.Client
// timeout), plus the signing call's own algod SuggestedParams round trip
// (no timeout of its own), plus headroom.
const SelfSettleAttemptBudget = 60 * time.Second

// SelfSettleRetryBudget is what a caller wrapping SettlePlatformFee or
// FundRunReserve in its own context.WithTimeout should use. Both call
// sites (tool402.go, runner.go) reference this constant rather than their
// own hardcoded duration, specifically so the budget can never silently
// drift out of sync with selfSettleMaxAttempts again the way it did before
// this constant existed: SettlePlatformFee's call site used to hardcode a
// flat 60s sized for exactly one attempt, sized before this file's retry
// loop existed at all -- once retries landed, that budget could starve a
// legitimate 2nd or 3rd attempt mid-flight, and a context deadline hit
// mid-Settle is indistinguishable from a genuinely lost response, so a
// starved retry would get misclassified as ErrSettlementIndeterminate
// (money's fate unknown) purely because the caller's own clock, not the
// facilitator's, ran out.
const SelfSettleRetryBudget = selfSettleMaxAttempts * SelfSettleAttemptBudget

func attemptSelfSettle(ctx context.Context, cfg RunPreFundConfig, publicPath, description, errPrefix string, amountUSDMicros int64) (string, error) {
	resourceURL := cfg.FrontendURL + publicPath
	reqs := x402.PaymentRequirements{
		Scheme:            "exact",
		Network:           cfg.RelayNetwork,
		MaxAmountRequired: strconv.FormatInt(amountUSDMicros, 10),
		// See RunPreFundConfig.FrontendURL's doc comment.
		Resource:          resourceURL,
		Description:       description,
		MimeType:          "application/json",
		PayTo:             cfg.PlatformWalletAddress,
		MaxTimeoutSeconds: 300,
		Asset:             strconv.FormatUint(cfg.ExpectedAssetID, 10),
		Extra: map[string]any{
			"asset":    strconv.FormatUint(cfg.ExpectedAssetID, 10),
			"feePayer": cfg.RelayFeePayer,
			"tag":      "x402-global-challenge",
			"decimals": 6,
		},
		// Bazaar discovery declaration on the struct actually POSTed to
		// /verify — extra.tag alone only attributes an already-discovered
		// route's activity to the challenge, it doesn't register the route.
		// Schema-valid shape (info.input.type/method + a schema sibling) --
		// see x402relay.go's bazaarDiscoveryExtension doc comment for why
		// the {info:{output:{...}}} shape this had before this fix failed
		// the facilitator's ajv validation unconditionally (no schema at
		// all, no info.input.type) and so never once cataloged, even though
		// verify/settle both succeeded and real money moved every time.
		Extensions: map[string]any{
			"bazaar": map[string]any{
				// Public /api proxy path, matching resourceURL above --
				// origin+routeTemplate has to resolve to a real URL, see
				// x402relay.go's routeTemplate comment.
				"routeTemplate": publicPath,
				"info": map[string]any{
					"input": map[string]any{"type": "http", "method": "GET"},
				},
				"schema": map[string]any{
					"$schema": "https://json-schema.org/draft/2020-12/schema",
					"type":    "object",
					"properties": map[string]any{
						"input": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"type":   map[string]any{"type": "string", "const": "http"},
								"method": map[string]any{"type": "string", "enum": []string{"GET", "HEAD", "DELETE"}},
							},
							"required":             []string{"type", "method"},
							"additionalProperties": false,
						},
					},
					"required": []string{"input"},
				},
			},
		},
	}

	group, idx, err := cfg.USDCSigner.SignUSDCPaymentGroup(ctx, cfg.PlatformSpendEncMnemonic, cfg.PlatformWalletAddress, cfg.ExpectedAssetID, uint64(amountUSDMicros), cfg.RelayFeePayer)
	if err != nil {
		return "", fmt.Errorf("%s: sign failed: %w", errPrefix, err)
	}
	payload := x402.PaymentPayload{
		X402Version: 2,
		Scheme:      "exact",
		Network:     cfg.RelayNetwork,
		Payload:     x402.PaymentGroup{PaymentGroup: group, PaymentIndex: idx},
		// Set authoritatively regardless of the fact that WE are both payer
		// and resource server here -- same reasoning as x402relay.go's
		// relaySelfSettle/relaySettleAndForward: the facilitator's discovery
		// extraction reads resource/extensions off the PAYLOAD, not off
		// PaymentRequirements above, so without these two fields this
		// settlement (real money, real facilitator round trip) still had
		// nothing for the catalog to key off, exactly like every other
		// settlement path did before its own matching fix today.
		Resource:   map[string]any{"url": resourceURL, "description": description, "mimeType": "application/json", "serviceName": "AgentMesh", "tags": []string{"x402-global-challenge"}},
		Extensions: reqs.Extensions,
		// Required field of the v2 payload schema -- see
		// x402.PaymentPayload.Accepted's doc comment. This path already set
		// a positive maxTimeoutSeconds on reqs (unlike x402relay.go's two
		// settle paths, which sent zero until this fix), so the projection
		// below is schema-valid as-is.
		Accepted: reqs.AcceptedV2(),
	}

	verifyResult, err := cfg.Facilitator.Verify(ctx, payload, reqs)
	if err != nil {
		return "", fmt.Errorf("%s: facilitator verify failed: %w", errPrefix, err)
	}
	if !verifyResult.IsValid {
		return "", fmt.Errorf("%s: payment invalid: %s", errPrefix, verifyResult.Invalid)
	}

	settleResult, err := cfg.Facilitator.Settle(ctx, payload, reqs)
	if err != nil {
		// Response never arrived -- settlement's fate is unknown, not
		// "failed". Wrapped so callers can tell this apart from a
		// definitive rejection and avoid releasing a reservation for money
		// that may have already moved.
		return "", fmt.Errorf("%s: facilitator settle response lost: %v: %w", errPrefix, err, ErrSettlementIndeterminate)
	}
	if !settleResult.Success {
		// A real, received response says it failed -- money definitively
		// never moved.
		return "", fmt.Errorf("%s: settlement failed: %s", errPrefix, settleResult.Error)
	}
	return settleResult.TxID, nil
}
