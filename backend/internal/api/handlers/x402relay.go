package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/respond"
	"github.com/agentmesh/backend/internal/x402"
)

const x402RelayHTTPTimeout = 15

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

	xPayment := r.Header.Get("X-Payment")
	if xPayment == "" {
		d.relayInboundChallenge(w, r, target)
		return
	}
	d.relaySettleAndForward(w, r, target, xPayment)
}

// relayInboundChallenge fetches the target's real 402 price and mirrors it
// back as our own v2 challenge, tagged for the challenge and paid to our
// platform wallet instead of the target's.
func (d *Deps) relayInboundChallenge(w http.ResponseWriter, r *http.Request, target string) {
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "target fetch failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	var targetChallenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &targetChallenge); err != nil || len(targetChallenge.Accepts) == 0 {
		respond.Error(w, http.StatusBadGateway, "target did not return a valid x402 challenge")
		return
	}
	targetAccept := targetChallenge.Accepts[0]
	amount, _ := targetAccept["maxAmountRequired"].(string)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]any{
		"x402Version": 2,
		"accepts": []map[string]any{{
			"scheme":            "exact",
			"network":           d.RelayNetwork,
			"maxAmountRequired": amount,
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

	reqs := x402.PaymentRequirements{
		Scheme:  "exact",
		Network: d.RelayNetwork,
		PayTo:   d.PlatformWalletAddress,
		Asset:   strconv.FormatUint(d.USDCAssetID, 10),
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

	ledgerRow, err := d.Store.RecordInboundSettlement(ctx, target, settleResult.TxID, 0)
	if err == db.ErrDuplicateSettlement {
		respond.Error(w, http.StatusConflict, "payment already processed")
		return
	}
	if err != nil {
		log.Printf("x402 relay: failed to record inbound settlement: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error recording settlement")
		return
	}

	d.payTargetAndRespond(w, r, target, ledgerRow.ID)
}

// payTargetAndRespond pays the real target from the platform wallet via the
// facilitator, then relays the target's paid response back to the caller.
// No refund path on failure: x402 has no chargeback primitive, and the
// inbound leg's attribution to us already landed regardless of this outcome.
func (d *Deps) payTargetAndRespond(w http.ResponseWriter, r *http.Request, target, ledgerID string) {
	ctx := r.Context()

	quoteReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	quoteResp, err := http.DefaultClient.Do(quoteReq)
	if err != nil {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusBadGateway, "target unreachable for payment: "+err.Error())
		return
	}
	var targetChallenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	body, _ := io.ReadAll(io.LimitReader(quoteResp.Body, 1<<20))
	quoteResp.Body.Close()
	if err := json.Unmarshal(body, &targetChallenge); err != nil || len(targetChallenge.Accepts) == 0 {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusBadGateway, "target did not return a valid x402 challenge on payment attempt")
		return
	}
	accept := targetChallenge.Accepts[0]
	payTo, _ := accept["payTo"].(string)
	assetStr, _ := accept["asset"].(string)
	amountStr, _ := accept["maxAmountRequired"].(string)
	assetID, _ := strconv.ParseUint(assetStr, 10, 64)
	amount, _ := strconv.ParseUint(amountStr, 10, 64)

	group, idx, err := d.USDCSigner.SignUSDCPaymentGroup(ctx, d.PlatformWalletEncMnemonic, payTo, assetID, amount, d.RelayFeePayer)
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
	payResp, err := http.DefaultClient.Do(payReq)
	if err != nil {
		d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "failed")
		respond.Error(w, http.StatusBadGateway, "paid request to target failed: "+err.Error())
		return
	}
	defer payResp.Body.Close()
	finalBody, _ := io.ReadAll(io.LimitReader(payResp.Body, 5<<20))

	d.Store.RecordOutboundSettlement(ctx, ledgerID, "", "settled")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(finalBody)
}
