package nodes

import (
	"context"
	"fmt"
	"strconv"

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

// runTotalPublicPath is SettleRunTotal's resource identity, same rationale
// as runFundingPublicPath/platformFeePublicPath -- a real, non-root,
// genuinely 402-answering path under our own branded origin, distinct from
// the other two so this settlement kind attributes separately.
const runTotalPublicPath = "/api/x402/relay/run-total"

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

// SettleRunTotal settles the whole run's non-tool402 billable total (agent
// platform-key LLM fees, action/google/http BYOK flat fees -- everything
// that otherwise only ever moves money inside the internal credit ledger)
// as one more real, direct Wallet 1 -> Wallet 2 payment, same mechanism as
// FundRunReserve/SettlePlatformFee. Tendril lease/rent cost is NOT included
// here -- it's charged against a wholly separate Tendril-credit pool
// (Store.ChargeTendrilCredit), never the AgentMesh credit ledger this
// settlement mirrors; only Tendril's own small gate fee is, and that
// already flows through the tool402 relay path (excluded here the same way
// as any other real tool402 spend). Called once
// per run, after all node-level billing has already been committed to the
// DB ledger -- this is an additive on-chain receipt for that already-final
// total, not a second charge: the user's credit balance was already
// decremented by the normal per-node debit calls. Real tool402 spend is
// excluded (it already gets its own real settlement via FundRunReserve /
// the per-call relay path) to avoid double-settling the same money on-chain.
func SettleRunTotal(ctx context.Context, cfg RunPreFundConfig, amountUSDMicros int64) (string, error) {
	return selfSettleWallet1ToWallet2(ctx, cfg, runTotalPublicPath,
		"AgentMesh workflow run total — lump-sum settlement of this run's platform-billed work",
		"run total settle", amountUSDMicros)
}

// selfSettleWallet1ToWallet2 signs, verifies, and settles one real GoPlausible
// payment for amountUSDMicros from the platform's own Wallet 1 spend wallet
// into Wallet 2 (cfg.PlatformWalletAddress) -- no external target involved,
// the platform is both payer and resource server. publicPath/description
// give the settlement its own distinct, genuinely-402-answering resource
// identity (see runFundingPublicPath's doc comment for why that has to be a
// real path under our own branded origin, not an opaque identifier).
// amountUSDMicros <= 0 is a no-op.
func selfSettleWallet1ToWallet2(ctx context.Context, cfg RunPreFundConfig, publicPath, description, errPrefix string, amountUSDMicros int64) (string, error) {
	if amountUSDMicros <= 0 {
		return "", nil
	}

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
		// Built by the shared BazaarDiscoveryExtension rather than spelled
		// out here: this block used to be a hand-maintained second copy of
		// the one in x402relay.go, which is exactly how the two drifted into
		// declaring method "GET" against an enum of ["GET","HEAD","DELETE"]
		// and silently failed the facilitator's catalog validator on every
		// settlement. RouteTemplate is the public /api proxy path, matching
		// resourceURL above -- origin+routeTemplate has to resolve to a real
		// URL. No queryParams (both self-settle routes take none) and no
		// output example (neither route returns a payable body; they answer
		// a static informational document).
		Extensions: BazaarDiscoveryExtension(BazaarDeclaration{RouteTemplate: publicPath}),
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
