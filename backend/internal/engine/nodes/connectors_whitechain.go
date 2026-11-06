package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"

	"github.com/agentmesh/backend/internal/chain"
	"github.com/agentmesh/backend/internal/models"
)

// Whitechain connectors: read-only chain lookups a workflow can run without
// paying anything. They read the Whitechain explorer's Blockscout v2 API,
// which already indexes balances, token holdings and transfers, so no
// archive node is needed.
//
//	whitechain_account       WBT + token balances for one address
//	whitechain_transactions  recent token transfers for one address
//	whitechain_token         metadata for one ERC-20 contract

var whitechainExplorerAPI = chain.DefaultExplorerURL + "/api/v2"

// SetWhitechainExplorer points the connectors at a Blockscout instance
// (WHITECHAIN_EXPLORER_URL). Empty keeps the default.
func SetWhitechainExplorer(base string) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base != "" {
		whitechainExplorerAPI = base + "/api/v2"
	}
}

const (
	whitechainTxLimitDefault = 10
	whitechainTxLimitMax     = 50
	maxTokenDecimals         = 36
)

func getExplorerJSON(ctx context.Context, path string, dst any) error {
	b, err := getRaw(ctx, whitechainExplorerAPI+path, map[string]string{"Accept": "application/json"}, "Whitechain explorer")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("Whitechain explorer: decode response: %w", err)
	}
	return nil
}

// formatBaseUnits renders an integer string of base units as an exact
// decimal string with the given decimals ("15000", 6 -> "0.015").
func formatBaseUnits(raw string, decimals int) string {
	n, ok := new(big.Int).SetString(strings.TrimSpace(raw), 10)
	if !ok {
		return ""
	}
	digits := n.String()
	neg := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")
	if decimals > 0 {
		if len(digits) <= decimals {
			digits = strings.Repeat("0", decimals-len(digits)+1) + digits
		}
		whole, frac := digits[:len(digits)-decimals], strings.TrimRight(digits[len(digits)-decimals:], "0")
		digits = whole
		if frac != "" {
			digits += "." + frac
		}
	}
	if neg {
		return "-" + digits
	}
	return digits
}

func whitechainAddressParam(node models.WorkflowNode, rc RunContexter) (string, error) {
	addr := strings.TrimSpace(resolveTemplate(configVal(node, "wcAddress", ""), rc))
	if addr == "" {
		return "", ErrActionSkipped
	}
	if !chain.IsAddress(addr) {
		return "", fmt.Errorf("whitechain: %q is not a 0x address (40 hex characters)", addr)
	}
	return addr, nil
}

type blockscoutToken struct {
	AddressHash string `json:"address_hash"`
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	Decimals    string `json:"decimals"`
	TotalSupply string `json:"total_supply"`
	Type        string `json:"type"`
	Holders     string `json:"holders_count"`
}

func (t blockscoutToken) decimals() (int, bool) {
	d, err := strconv.Atoi(t.Decimals)
	if err != nil || d < 0 || d > maxTokenDecimals {
		return 0, false
	}
	return d, true
}

func fetchWhitechainAccount(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	addr, err := whitechainAddressParam(node, rc)
	if err == ErrActionSkipped {
		return "whitechain_skipped_no_address", err
	} else if err != nil {
		return nil, err
	}
	var acct struct {
		Hash        string `json:"hash"`
		CoinBalance string `json:"coin_balance"`
		IsContract  bool   `json:"is_contract"`
	}
	if err := getExplorerJSON(ctx, "/addresses/"+url.PathEscape(addr), &acct); err != nil {
		return nil, err
	}
	var holdings []struct {
		Value string          `json:"value"`
		Token blockscoutToken `json:"token"`
	}
	if err := getExplorerJSON(ctx, "/addresses/"+url.PathEscape(addr)+"/token-balances", &holdings); err != nil {
		return nil, err
	}
	tokens := make([]map[string]any, 0, len(holdings))
	usdt := "0"
	for _, h := range holdings {
		row := map[string]any{
			"token":           h.Token.AddressHash,
			"symbol":          h.Token.Symbol,
			"name":            h.Token.Name,
			"type":            h.Token.Type,
			"amountBaseUnits": h.Value,
		}
		if d, ok := h.Token.decimals(); ok {
			row["decimals"] = d
			row["amount"] = formatBaseUnits(h.Value, d)
		}
		if chain.IsUSDT(h.Token.AddressHash) {
			usdt = formatBaseUnits(h.Value, chain.TokenDecimals)
		}
		tokens = append(tokens, row)
	}
	if acct.CoinBalance == "" {
		acct.CoinBalance = "0"
	}
	return map[string]any{
		"network":     chain.Network,
		"address":     acct.Hash,
		"isContract":  acct.IsContract,
		"wbt":         formatBaseUnits(acct.CoinBalance, chain.NativeDecimals),
		"wbtWei":      acct.CoinBalance,
		"usdt":        usdt,
		"tokens":      tokens,
		"explorerUrl": chain.AddressURL(acct.Hash),
	}, nil
}

func fetchWhitechainTransactions(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	addr, err := whitechainAddressParam(node, rc)
	if err == ErrActionSkipped {
		return "whitechain_skipped_no_address", err
	} else if err != nil {
		return nil, err
	}
	limit := whitechainTxLimitDefault
	if raw := strings.TrimSpace(resolveTemplate(configVal(node, "wcTxLimit", ""), rc)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("whitechain: wcTxLimit must be a positive whole number, got %q", raw)
		}
		limit = min(n, whitechainTxLimitMax)
	}
	path := "/addresses/" + url.PathEscape(addr) + "/token-transfers?type=ERC-20"
	token := strings.TrimSpace(resolveTemplate(configVal(node, "wcToken", ""), rc))
	if token != "" {
		if !chain.IsAddress(token) {
			return nil, fmt.Errorf("whitechain: wcToken %q is not a token contract address", token)
		}
		path += "&token=" + url.QueryEscape(token)
	}
	var page struct {
		Items []struct {
			Hash      string `json:"transaction_hash"`
			Block     int64  `json:"block_number"`
			Timestamp string `json:"timestamp"`
			Method    string `json:"method"`
			From      struct {
				Hash string `json:"hash"`
			} `json:"from"`
			To struct {
				Hash string `json:"hash"`
			} `json:"to"`
			Token blockscoutToken `json:"token"`
			Total struct {
				Value    string `json:"value"`
				Decimals string `json:"decimals"`
			} `json:"total"`
		} `json:"items"`
	}
	if err := getExplorerJSON(ctx, path, &page); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, min(limit, len(page.Items)))
	for _, it := range page.Items {
		if len(rows) >= limit {
			break
		}
		dir := "other"
		switch {
		case chain.SameAddress(it.From.Hash, addr):
			dir = "out"
		case chain.SameAddress(it.To.Hash, addr):
			dir = "in"
		}
		row := map[string]any{
			"hash":            it.Hash,
			"block":           it.Block,
			"time":            it.Timestamp,
			"method":          it.Method,
			"direction":       dir,
			"from":            it.From.Hash,
			"to":              it.To.Hash,
			"token":           it.Token.AddressHash,
			"symbol":          it.Token.Symbol,
			"amountBaseUnits": it.Total.Value,
			"explorerUrl":     chain.TxURL(it.Hash),
		}
		if d, ok := it.Token.decimals(); ok {
			row["amount"] = formatBaseUnits(it.Total.Value, d)
		}
		rows = append(rows, row)
	}
	return map[string]any{
		"network":   chain.Network,
		"address":   addr,
		"transfers": rows,
		"count":     len(rows),
	}, nil
}

func fetchWhitechainToken(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	token := strings.TrimSpace(resolveTemplate(configVal(node, "wcToken", ""), rc))
	if token == "" {
		token = chain.USDTAddress
	}
	if !chain.IsAddress(token) {
		return nil, fmt.Errorf("whitechain: wcToken %q is not a token contract address", token)
	}
	var t blockscoutToken
	if err := getExplorerJSON(ctx, "/tokens/"+url.PathEscape(token), &t); err != nil {
		return nil, err
	}
	out := map[string]any{
		"network":        chain.Network,
		"token":          t.AddressHash,
		"name":           t.Name,
		"symbol":         t.Symbol,
		"type":           t.Type,
		"holders":        t.Holders,
		"totalBaseUnits": t.TotalSupply,
		"x402Payable":    chain.IsUSDT(t.AddressHash),
		"explorerUrl":    chain.DefaultExplorerURL + "/token/" + t.AddressHash,
	}
	if d, ok := t.decimals(); ok {
		out["decimals"] = d
		out["total"] = formatBaseUnits(t.TotalSupply, d)
	}
	return out, nil
}
