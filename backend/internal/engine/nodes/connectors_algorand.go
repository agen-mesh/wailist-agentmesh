package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// algodAPIBase is the Algorand node this connector reads current state from.
// Set once at startup from ALGOD_URL, mirroring platformKeysForTools rather
// than widening every call site for a value that is genuinely process-wide.
//
// Deliberately a plain HTTP base and not the go-algorand-sdk client that
// internal/wallet holds: that client exists to SIGN, and a read needs none of
// it. Going through getAndDecode instead keeps this connector inside the same
// timeout, SSRF-safe dialer, error sanitising and retry classification every
// other connector gets.
var algodAPIBase string

// SetAlgorandBases installs the algod base URL. Blank disables the connector,
// which then fails closed naming the setting.
func SetAlgorandBases(algod string) { algodAPIBase = strings.TrimRight(strings.TrimSpace(algod), "/") }

// microAlgosPerAlgo converts the only unit algod reports into the only unit a
// person uses.
const microAlgosPerAlgo = 1_000_000

// fetchAlgorandAccount returns an address's ALGO balance and its ASA
// holdings.
//
// The output is flattened on purpose. algod nests holdings under
// account.assets[].asset-id, and every extra level is a jsonPath the builder
// can get wrong -- which it demonstrably does, a real test run failing with
// `path "prices" descends past a scalar`. Balances are converted to whole
// ALGO here for the same class of reason: an agent handed microalgos reports
// them as ALGO.
//
// algod answers current state only. Transaction history needs an indexer, a
// separate service this connector does not talk to; the catalog note sends
// the builder to the x402 Bazaar for that instead.
func fetchAlgorandAccount(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	address := strings.TrimSpace(resolveTemplate(configVal(node, "algoAddress", ""), rc))
	if address == "" {
		return "algorand_skipped_no_address", ErrActionSkipped
	}
	if algodAPIBase == "" {
		return nil, fmt.Errorf("algorand: no Algorand node is configured -- set ALGOD_URL")
	}
	raw, err := getAndDecode(ctx, algodAPIBase+"/v2/accounts/"+url.PathEscape(address), nil, "Algorand")
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("algorand: %w", err)
	}
	var body struct {
		Address    string `json:"address"`
		Amount     int64  `json:"amount"`
		MinBalance int64  `json:"min-balance"`
		Assets     []struct {
			AssetID int64 `json:"asset-id"`
			Amount  int64 `json:"amount"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, fmt.Errorf("algorand: %w", err)
	}
	// Non-nil even when empty: nil marshals to null, and an agent handed null
	// says "unknown" where the true answer is "none".
	assets := make([]map[string]any, 0, len(body.Assets))
	for _, a := range body.Assets {
		assets = append(assets, map[string]any{"assetId": a.AssetID, "amount": a.Amount})
	}
	return map[string]any{
		"address":    body.Address,
		"algo":       float64(body.Amount) / microAlgosPerAlgo,
		"minBalance": float64(body.MinBalance) / microAlgosPerAlgo,
		"assets":     assets,
	}, nil
}
