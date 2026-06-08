package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	if resp.StatusCode != http.StatusPaymentRequired {
		return map[string]any{"price": "0", "unit": "", "network": "", "recipient": ""}, nil
	}
	return parsePaymentHeader(resp), nil
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

	quote := parsePaymentHeader(resp)
	resp.Body.Close() // done with the 402 response

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

	// Retry the original request with the payment proof header.
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, node.Endpoint, nil)
	req2.Header.Set("X-Payment-Txid", txID)
	resp2, err := toolHTTPClient.Do(req2)
	if err != nil {
		return map[string]any{"status": "payment_sent", "txId": txID, "error": "retry request failed: " + err.Error()}, nil
	}
	defer resp2.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp2.Body, httpResponseLimit))
	var retryResult any
	if json.Unmarshal(b, &retryResult) == nil {
		return map[string]any{"status": "payment_sent", "txId": txID, "response": retryResult}, nil
	}
	return map[string]any{"status": "payment_sent", "txId": txID, "response": string(b)}, nil
}

func parsePaymentHeader(resp *http.Response) map[string]any {
	header := resp.Header.Get("X-Payment-Required")
	if header == "" {
		header = resp.Header.Get("WWW-Authenticate")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(header), &result); err != nil {
		result = map[string]any{"raw": header}
	}
	return result
}
