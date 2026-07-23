package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/agentmesh/backend/internal/models"
)

// WalletSigner signs and submits an Algorand payment transaction.
// Satisfied by *wallet.Service.
type WalletSigner interface {
	SignAndSendPayment(ctx context.Context, encMnemonic, toAddress string, microAlgo uint64) (string, error)
}

func QuoteX402(ctx context.Context, rawURL string) (map[string]any, error) {
	if err := urlValidator(rawURL); err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Always attempt to parse payment info from header+body regardless of status.
	// Proxies (Cloudflare tunnels) may rewrite 402 → 200/503 or strip headers.
	quote := parsePaymentHeader(resp)
	if _, hasPrice := quote["price"]; hasPrice {
		return quote, nil
	}
	return map[string]any{"price": "0", "unit": "", "network": "", "recipient": ""}, nil
}

func ExecuteTool402(ctx context.Context, node models.WorkflowNode, rc RunContexter, wallet models.AgentWallet, signer WalletSigner) (any, error) {
	if err := urlValidator(node.Endpoint); err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, node.Endpoint, nil)
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusPaymentRequired {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
		var result any
		if json.Unmarshal(b, &result) == nil {
			return result, nil
		}
		return string(b), nil
	}

	quote := parsePaymentHeader(resp) // reads body internally
	resp.Body.Close()

	if wallet.EncryptedMnemonic == "" || signer == nil {
		return map[string]any{"error": "payment required but no agent wallet configured", "quote": quote}, nil
	}

	priceStr, _ := quote["price"].(string)
	recipient, _ := quote["recipient"].(string)
	if recipient == "" {
		return nil, fmt.Errorf("x402: no recipient address in payment header")
	}
	priceFloat, err := strconv.ParseFloat(priceStr, 64)
	if err != nil || priceFloat <= 0 {
		return nil, fmt.Errorf("x402: invalid price %q", priceStr)
	}
	microAlgo := uint64(priceFloat * 1e6)

	txID, err := signer.SignAndSendPayment(ctx, wallet.EncryptedMnemonic, recipient, microAlgo)
	if err != nil {
		return nil, fmt.Errorf("x402 payment failed: %w", err)
	}

	algoAmount := fmt.Sprintf("%.6f", float64(microAlgo)/1e6)
	explorerURL := "https://lora.algokit.io/testnet/transaction/" + txID

	// Retry the original request with the payment proof header.
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, node.Endpoint, nil)
	req2.Header.Set("X-Payment-Txid", txID)
	resp2, err := toolHTTPClient.Do(req2)
	if err != nil {
		return map[string]any{"status": "payment_sent", "txId": txID, "amount": algoAmount, "explorerURL": explorerURL, "error": "retry request failed: " + err.Error()}, nil
	}
	defer resp2.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp2.Body, httpResponseLimit))
	var retryResult any
	if json.Unmarshal(b, &retryResult) == nil {
		return map[string]any{"status": "payment_sent", "txId": txID, "amount": algoAmount, "explorerURL": explorerURL, "response": retryResult}, nil
	}
	return map[string]any{"status": "payment_sent", "txId": txID, "amount": algoAmount, "explorerURL": explorerURL, "response": string(b)}, nil
}

func parsePaymentHeader(resp *http.Response) map[string]any {
	// Try header first (direct connections). Cloudflare and other proxies may
	// strip non-standard response headers, so fall back to the response body.
	header := resp.Header.Get("X-Payment-Required")
	if header == "" {
		header = resp.Header.Get("WWW-Authenticate")
	}
	var result map[string]any
	if header != "" {
		if err := json.Unmarshal([]byte(header), &result); err == nil {
			return result
		}
	}
	// Body fallback: our server returns {"error":"Payment required","payment":{...}}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
	var envelope struct {
		Payment map[string]any `json:"payment"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Payment != nil {
		return envelope.Payment
	}
	// Last resort: try parsing body directly as the payment object
	if err := json.Unmarshal(body, &result); err == nil {
		return result
	}
	return map[string]any{"raw": header}
}

// USDCGroupSigner signs a gasless USDC atomic-payment group for the relay's
// X-Payment header. Satisfied by *wallet.Service (SignUSDCPaymentGroup).
type USDCGroupSigner interface {
	SignUSDCPaymentGroup(ctx context.Context, encMnemonic, payTo string, assetID, amountMicros uint64, feePayerAddr string) ([]string, int, error)
}

// ExecuteTool402V2 is the entry point runner.go calls for tool402 nodes. It
// inspects the target's 402 quote shape: a real x402 v2 challenge (accepts[])
// is routed through the AgentMesh relay so both payment legs are real,
// GoPlausible-settled, and attributable to us as an orchestrator entry. The
// legacy flat-quote dialect (no accepts[]) bypasses the relay entirely and
// keeps today's direct-pay behavior unchanged — it was never
// GoPlausible-compliant and isn't becoming so.
func ExecuteTool402V2(ctx context.Context, node models.WorkflowNode, rc RunContexter, aw models.AgentWallet, signer WalletSigner, usdcSigner USDCGroupSigner, relayBaseURL string) (any, error) {
	if err := urlValidator(node.Endpoint); err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, node.Endpoint, nil)
	resp, err := toolHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusPaymentRequired {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
		var result any
		if json.Unmarshal(b, &result) == nil {
			return result, nil
		}
		return string(b), nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, httpResponseLimit))
	resp.Body.Close()
	var v2Challenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	if json.Unmarshal(body, &v2Challenge) == nil && len(v2Challenge.Accepts) > 0 {
		return executeTool402V2Relay(ctx, node, aw, usdcSigner, relayBaseURL)
	}

	// Legacy flat-quote dialect: unchanged direct-pay path.
	return ExecuteTool402(ctx, node, rc, aw, signer)
}

func executeTool402V2Relay(ctx context.Context, node models.WorkflowNode, aw models.AgentWallet, usdcSigner USDCGroupSigner, relayBaseURL string) (any, error) {
	if aw.EncryptedMnemonic == "" || usdcSigner == nil {
		return map[string]any{"error": "payment required but no agent wallet configured"}, nil
	}

	relayURL := relayBaseURL + "/x402/relay?target=" + url.QueryEscape(node.Endpoint)

	quoteReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, relayURL, nil)
	quoteResp, err := toolHTTPClient.Do(quoteReq)
	if err != nil {
		return nil, fmt.Errorf("x402 relay quote failed: %w", err)
	}
	quoteBody, _ := io.ReadAll(io.LimitReader(quoteResp.Body, httpResponseLimit))
	quoteResp.Body.Close()

	var relayChallenge struct {
		Accepts []map[string]any `json:"accepts"`
	}
	if json.Unmarshal(quoteBody, &relayChallenge) != nil || len(relayChallenge.Accepts) == 0 {
		return nil, fmt.Errorf("x402 relay: invalid challenge response")
	}
	accept := relayChallenge.Accepts[0]
	payTo, _ := accept["payTo"].(string)
	assetStr, _ := accept["asset"].(string)
	amountStr, _ := accept["maxAmountRequired"].(string)
	var feePayer string
	if extra, ok := accept["extra"].(map[string]any); ok {
		feePayer, _ = extra["feePayer"].(string)
	}
	assetID, _ := strconv.ParseUint(assetStr, 10, 64)
	amount, _ := strconv.ParseUint(amountStr, 10, 64)

	group, idx, err := usdcSigner.SignUSDCPaymentGroup(ctx, aw.EncryptedMnemonic, payTo, assetID, amount, feePayer)
	if err != nil {
		return nil, fmt.Errorf("x402 relay payment signing failed: %w", err)
	}
	xPayment, _ := json.Marshal(map[string]any{
		"x402Version": 2, "scheme": "exact",
		"payload": map[string]any{"paymentGroup": group, "paymentIndex": idx},
	})

	payReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, relayURL, nil)
	payReq.Header.Set("X-Payment", string(xPayment))
	payResp, err := toolHTTPClient.Do(payReq)
	if err != nil {
		return nil, fmt.Errorf("x402 relay payment request failed: %w", err)
	}
	defer payResp.Body.Close()
	finalBody, _ := io.ReadAll(io.LimitReader(payResp.Body, httpResponseLimit))

	var result any
	if json.Unmarshal(finalBody, &result) == nil {
		return result, nil
	}
	return string(finalBody), nil
}
