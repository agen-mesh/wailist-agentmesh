package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/respond"
	"github.com/agentmesh/backend/internal/x402"
)

// X402Relay is the orchestrator's own paid endpoint. It has no fixed price:
// the price it charges the caller is whatever the target endpoint (given via
// ?target=) actually charges. This is what makes the relay generic across
// every x402 endpoint in the GoPlausible marketplace, not just a fixed set.
//
// Flow: no X-Payment header -> fetch target's real 402, mirror it back as our
// own v2/USDC/tagged challenge (payTo = platform wallet). X-Payment present ->
// verify+settle the inbound payment via the facilitator (credited to us),
// then pay the target from the platform wallet (credited to them), then
// relay the target's paid response back to the caller.
func (d *Deps) X402Relay(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		respond.Error(w, http.StatusBadRequest, "target query param required")
		return
	}
	// target is caller-supplied and this route is public/unauthenticated —
	// without this check, callers could make the relay fetch or pay
	// arbitrary internal/private addresses (SSRF). Same guard applied to
	// every tool402 node's target before Task 6 wires it through here.
	if err := nodes.ValidateURL(target); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid target: "+err.Error())
		return
	}

	xPayment := r.Header.Get("X-Payment")
	if xPayment == "" {
		d.relayInboundChallenge(w, r, target)
		return
	}
	d.relaySettleAndForward(w, r, target, xPayment)
}

// targetPriceQuote is the subset of a target's x402 402 response the relay
// cares about.
type targetPriceQuote struct {
	PayTo             string
	Asset             string
	MaxAmountRequired string
}

// fetchTargetPriceQuote issues an unauthenticated GET to the caller-supplied
// target (via the SSRF-safe shared client, which also enforces a 10s dial+
// request timeout — see nodes.toolHTTPClient) and parses its x402 402
// challenge.
//
// This is called independently from two places: relayInboundChallenge (to
// mirror the price to the caller on the no-payment leg) and
// relaySettleAndForward (to learn the authoritative price to enforce and
// record before settling the inbound payment). Those two calls are separate,
// unrelated HTTP requests (no-payment challenge vs. with-payment settle) with
// no shared state between them, so the price genuinely has to be re-fetched
// across that boundary rather than trusted from the earlier call.
//
// Deliberately NOT called a second time from payTargetAndRespond: target is
// caller-supplied and this route is public/unauthenticated, so re-fetching a
// second, independent quote at pay-time would let a malicious target answer
// cheaply the first time and expensively (and/or to a different payTo) the
// second time, draining the platform wallet for more than was ever collected
// from the caller. relaySettleAndForward fetches the quote exactly once per
// relay cycle and passes that same value into payTargetAndRespond.
func fetchTargetPriceQuote(ctx context.Context, target string) (targetPriceQuote, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return targetPriceQuote{}, err
	}
	resp, err := nodes.SafeHTTPClient().Do(req)
	if err != nil {
		return targetPriceQuote{}, err
	}
	defer resp.Body.Close()

	var parsed struct {
		Accepts []map[string]any `json:"accepts"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Accepts) == 0 {
		return targetPriceQuote{}, fmt.Errorf("target did not return a valid x402 challenge")
	}
	accept := parsed.Accepts[0]
	payTo, _ := accept["payTo"].(string)
	asset, _ := accept["asset"].(string)
	amount, _ := accept["maxAmountRequired"].(string)
	return targetPriceQuote{PayTo: payTo, Asset: asset, MaxAmountRequired: amount}, nil
}

// relayInboundChallenge fetches the target's real 402 price and mirrors it
// back as our own v2 challenge, tagged for the challenge and paid to our
// platform wallet instead of the target's.
func (d *Deps) relayInboundChallenge(w http.ResponseWriter, r *http.Request, target string) {
	quote, err := fetchTargetPriceQuote(r.Context(), target)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "target fetch failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]any{
		"x402Version": 2,
		"accepts": []map[string]any{{
			"scheme":            "exact",
			"network":           d.RelayNetwork,
			"maxAmountRequired": quote.MaxAmountRequired,
			"resource":          target,
			"payTo":             d.PlatformWalletAddress,
			"maxTimeoutSeconds": 300,
			"asset":             strconv.FormatUint(d.USDCAssetID, 10),
			"extra": map[string]any{
				"asset":    strconv.FormatUint(d.USDCAssetID, 10),
				"feePayer": d.RelayFeePayer,
				"tag":      "x402-global-challenge",
				"decimals": 6,
			},
		}},
	})
}

// relaySettleAndForward verifies+settles the caller's inbound payment, then
// pays the real target from the platform wallet, then relays the target's
// paid response back. Both settlements are real, GoPlausible-facilitated,
// mainnet payments — this is what earns orchestrator-entry attribution.
func (d *Deps) relaySettleAndForward(w http.ResponseWriter, r *http.Request, target, xPaymentHeader string) {
	ctx := r.Context()

	var payload x402.PaymentPayload
	if err := json.Unmarshal([]byte(xPaymentHeader), &payload); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid X-Payment payload")
		return
	}

	// Re-fetch the target's own 402 to learn the authoritative current
	// price. This is what lets us set MaxAmountRequired below (so the
	// facilitator actually enforces the quoted price instead of trusting
	// whatever the caller's payment payload claims) and what lets us record
	// the real settled amount in the ledger instead of a hardcoded 0.
	quote, err := fetchTargetPriceQuote(ctx, target)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "target fetch failed: "+err.Error())
		return
	}
	amountAssetMicros, err := strconv.ParseInt(quote.MaxAmountRequired, 10, 64)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "target returned invalid maxAmountRequired: "+err.Error())
		return
	}

	reqs := x402.PaymentRequirements{
		Scheme:            "exact",
		Network:           d.RelayNetwork,
		PayTo:             d.PlatformWalletAddress,
		Asset:             strconv.FormatUint(d.USDCAssetID, 10),
		MaxAmountRequired: quote.MaxAmountRequired,
	}

	verifyResult, err := d.FacilitatorClient.Verify(ctx, payload, reqs)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "facilitator verify failed: "+err.Error())
		return
	}
	if !verifyResult.IsValid {
		respond.Error(w, http.StatusPaymentRequired, "payment invalid: "+verifyResult.Invalid)
		return
	}

	settleResult, err := d.FacilitatorClient.Settle(ctx, payload, reqs)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "facilitator settle failed: "+err.Error())
		return
	}
	if !settleResult.Success {
		respond.Error(w, http.StatusPaymentRequired, "settlement failed: "+settleResult.Error)
		return
	}

	ledgerRow, err := d.Store.RecordInboundSettlement(ctx, target, settleResult.TxID, amountAssetMicros)
	if err == db.ErrDuplicateSettlement {
		respond.Error(w, http.StatusConflict, "payment already processed")
		return
	}
	if err != nil {
		log.Printf("x402 relay: failed to record inbound settlement: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error recording settlement")
		return
	}

	d.payTargetAndRespond(w, r, target, ledgerRow.ID, quote)
}

// payTargetAndRespond pays the real target from the platform wallet via the
// facilitator, then relays the target's paid response back to the caller.
// No refund path on failure: x402 has no chargeback primitive, and the
// inbound leg's attribution to us already landed regardless of this outcome.
//
// It takes the already-fetched targetPriceQuote from relaySettleAndForward
// (its only caller) rather than re-fetching the caller-supplied target
// itself. This is deliberate: target is arbitrary and attacker-controlled on
// this public, unauthenticated route, so re-fetching it a second time here
// would let a malicious target answer the first (enforcement/recording)
// fetch cheaply and this second (pay-time) fetch with an inflated amount
// and/or a different payTo — causing the platform wallet to sign and
// broadcast a payment for more than was ever collected from the caller, to
// an address the caller chose. One quote, fetched once per relay cycle, used
// for both enforcement/recording and the actual outbound payment closes that
// gap.
//
// Outbound tx id: paying the target here goes over the target's own
// X-Payment header directly, not through our own FacilitatorClient, so there
// is no SettleResult (and therefore no facilitator-issued transaction id) on
// this leg — the target's paid HTTP response carries no standardized txid
// reference either. RecordOutboundSettlement is called with an empty
// outbound tx id below; that is a real gap in observability given the
// current architecture (the relay pays the target directly rather than via
// a second facilitator round-trip from our side), not an oversight, and not
// something to paper over with a fabricated id.
func (d *Deps) payTargetAndRespond(w http.ResponseWriter, r *http.Request, target, ledgerID string, quote targetPriceQuote) {
	ctx := r.Context()

	assetID, _ := strconv.ParseUint(quote.Asset, 10, 64)
	amount, _ := strconv.ParseUint(quote.MaxAmountRequired, 10, 64)

	// quote.Asset comes straight from the target's own (caller-supplied,
	// unauthenticated) 402 response. The inbound settlement side is already
	// anchored to the platform's own asset id (reqs.Asset =
	// strconv.FormatUint(d.USDCAssetID, 10) in relaySettleAndForward), but
	// nothing enforced that here — a malicious target could consistently
	// quote a different asset id (one it controls, or one with no value at
	// all) and the platform wallet would sign and broadcast a real payment
	// in that asset. Refuse before ever touching the signer.
	if assetID != d.USDCAssetID {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusBadGateway, "target quoted an unexpected asset id")
		return
	}

	group, idx, err := d.USDCSigner.SignUSDCPaymentGroup(ctx, d.PlatformWalletEncMnemonic, quote.PayTo, assetID, amount, d.RelayFeePayer)
	if err != nil {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusInternalServerError, "failed to sign outbound payment: "+err.Error())
		return
	}
	xPaymentOut, _ := json.Marshal(map[string]any{
		"x402Version": 2, "scheme": "exact", "network": d.RelayNetwork,
		"payload": map[string]any{"paymentGroup": group, "paymentIndex": idx},
	})

	payReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	payReq.Header.Set("X-Payment", string(xPaymentOut))
	payResp, err := nodes.SafeHTTPClient().Do(payReq)
	if err != nil {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusBadGateway, "paid request to target failed: "+err.Error())
		return
	}
	defer payResp.Body.Close()
	finalBody, _ := io.ReadAll(io.LimitReader(payResp.Body, 5<<20))

	// See the empty outbound-tx-id note in the function doc comment above:
	// there is no facilitator-issued outbound transaction id available at
	// this call site with the current design.
	d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "settled")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(finalBody)
}
