package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
)

func QuoteX402(ctx context.Context, url string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		return map[string]any{"price": "0", "unit": "", "network": "", "recipient": ""}, nil
	}
	return parsePaymentHeader(resp), nil
}

func ExecuteTool402(ctx context.Context, node models.WorkflowNode, rc RunContexter, wallet models.AgentWallet, store *db.Store) (any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, node.Endpoint, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPaymentRequired {
		b, _ := io.ReadAll(resp.Body)
		var result any
		if json.Unmarshal(b, &result) == nil {
			return result, nil
		}
		return string(b), nil
	}

	quote := parsePaymentHeader(resp)
	if wallet.EncryptedMnemonic == "" || store == nil {
		return map[string]any{"error": "payment required but no agent wallet available", "quote": quote}, nil
	}

	priceStr, _ := quote["price"].(string)
	priceFloat, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid price %q: %w", priceStr, err)
	}
	_ = uint64(priceFloat * 1e6) // microAlgo — actual signing deferred to Phase 2

	return map[string]any{"status": "payment_sent", "pricePaid": priceStr, "quote": quote}, nil
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
