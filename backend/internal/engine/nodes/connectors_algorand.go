package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
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

// indexerAPIBase is the Algorand indexer transaction history is read from,
// set at startup from ALGORAND_INDEXER_URL.
//
// A second setting rather than a path on algodAPIBase, because they are
// genuinely two services: algod holds current state only and has no
// transaction history at all, while an indexer is a separate process with
// its own database and its own URL. They must still name the same chain,
// which is why both are set in one call from one place.
var indexerAPIBase string

// AlgorandIndexerDefault is the public indexer for a declared network, used
// when ALGORAND_INDEXER_URL is not set.
//
// Keyed on ALGORAND_NETWORK rather than hard-coded to one chain, the same way
// the relay's USDC asset id and CAIP-2 network are: a mainnet deployment that
// predates this setting sets the network and algod but not the indexer, and a
// fixed testnet default would then read mainnet balances beside testnet
// history with no error anywhere.
func AlgorandIndexerDefault(network string) string {
	if network == "mainnet" {
		return "https://mainnet-idx.algonode.cloud"
	}
	return "https://testnet-idx.algonode.cloud"
}

// SetAlgorandBases installs the algod and indexer base URLs. Either blank
// disables the connectors that need it, which then fail closed naming the
// setting they want.
func SetAlgorandBases(algod, indexer string) {
	algodAPIBase = strings.TrimRight(strings.TrimSpace(algod), "/")
	indexerAPIBase = strings.TrimRight(strings.TrimSpace(indexer), "/")
}

// algoDecimals is how many decimal places separate the only unit algod
// reports (microalgos) from the only unit a person uses. ALGO is just an
// asset with six decimals as far as formatting goes, so it goes through
// formatBaseUnits like every ASA holding rather than through a float: total
// supply is 10^16 microalgos and float64 stops being exact at about
// 9.007e15, which is every exchange and foundation wallet.
const algoDecimals = 6

// maxAlgorandAssetLookups caps the per-holding /v2/assets calls one read
// makes. An account can hold thousands of ASAs, and each lookup is a round
// trip; holdings past the cap are still listed, in base units only, and
// counted in unresolvedAssets so the gap is visible rather than silent.
const maxAlgorandAssetLookups = 20

// maxASADecimals is the protocol ceiling on an ASA's decimals. Anything above
// it is not a real asset, so it is reported unresolved rather than formatted.
const maxASADecimals = 19

// getIndexerJSON is getAlgodJSON against the indexer instead. Same reasons
// for not going through getAndDecode: an indexer's amounts and rounds are
// uint64 and a float round trip would round them.
func getIndexerJSON(ctx context.Context, path string, dst any) error {
	b, err := getRaw(ctx, indexerAPIBase+path, map[string]string{"Accept": "application/json"}, "Algorand indexer")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("Algorand indexer: decode response: %w", err)
	}
	return nil
}

// getAlgodJSON GETs an algod path and decodes the body straight into dst.
//
// Deliberately not getAndDecode: that decodes into `any` first, which turns
// every number into a float64. algod amounts are uint64, so a round trip
// through float silently rounds anything past 2^53 and fails outright on
// anything past the int64 range -- both reachable for a large balance of a
// high-decimal token. getRaw applies the same SSRF guard, status handling
// and retry classification getJSON does, and leaves the body as bytes.
func getAlgodJSON(ctx context.Context, path string, dst any) error {
	b, err := getRaw(ctx, algodAPIBase+path, map[string]string{"Accept": "application/json"}, "Algorand")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("Algorand: decode response: %w", err)
	}
	return nil
}

// algorandAssetParams is the part of GET /v2/assets/{id} this connector uses.
type algorandAssetParams struct {
	Params struct {
		Decimals uint64 `json:"decimals"`
		UnitName string `json:"unit-name"`
		Name     string `json:"name"`
	} `json:"params"`
}

// formatBaseUnits renders an ASA amount in whole units as an exact decimal
// string: 1500000 at 6 decimals is "1.5". Integer arithmetic only, because a
// float cannot hold every uint64 and a balance is not the place to round.
func formatBaseUnits(v, decimals uint64) string {
	digits := strconv.FormatUint(v, 10)
	if decimals == 0 {
		return digits
	}
	d := int(decimals)
	if len(digits) <= d {
		digits = strings.Repeat("0", d-len(digits)+1) + digits
	}
	whole, frac := digits[:len(digits)-d], strings.TrimRight(digits[len(digits)-d:], "0")
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}

// fetchAlgorandAccount returns an address's ALGO balance and its ASA
// holdings.
//
// The output is flattened on purpose. algod nests holdings under
// account.assets[].asset-id, and every extra level is a jsonPath the builder
// can get wrong -- which it demonstrably does, a real test run failing with
// `path "prices" descends past a scalar`. Amounts are converted to whole
// units here for the same class of reason: an agent handed base units
// reports them as whole units, so 1.5 USDC would read as 1,500,000 USDC.
// Every converted amount is an exact decimal string with the exact integer
// beside it (algo/algoMicro, amount/amountBaseUnits), because a balance is
// not the place to round and float64 cannot hold every uint64: ALGO supply
// alone is 10^16 microalgos, past where float64 stays exact.
//
// algod answers current state only. Transaction history needs an indexer, a
// separate service this connector does not talk to -- see
// fetchAlgorandTransactions, which does.
func fetchAlgorandAccount(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	address := strings.TrimSpace(resolveTemplate(configVal(node, "algoAddress", ""), rc))
	if address == "" {
		return "algorand_skipped_no_address", ErrActionSkipped
	}
	if algodAPIBase == "" {
		return nil, fmt.Errorf("algorand: no Algorand node is configured -- set ALGOD_URL")
	}
	var body struct {
		Address    string `json:"address"`
		Amount     uint64 `json:"amount"`
		MinBalance uint64 `json:"min-balance"`
		Assets     []struct {
			AssetID uint64 `json:"asset-id"`
			Amount  uint64 `json:"amount"`
		} `json:"assets"`
	}
	if err := getAlgodJSON(ctx, "/v2/accounts/"+url.PathEscape(address), &body); err != nil {
		return nil, err
	}
	// Non-nil even when empty: nil marshals to null, and an agent handed null
	// says "unknown" where the true answer is "none".
	assets := make([]map[string]any, 0, len(body.Assets))
	unresolved := 0
	for i, a := range body.Assets {
		holding := map[string]any{"assetId": a.AssetID, "amountBaseUnits": a.Amount}
		assets = append(assets, holding)
		if i >= maxAlgorandAssetLookups {
			unresolved++
			continue
		}
		var info algorandAssetParams
		if err := getAlgodJSON(ctx, "/v2/assets/"+strconv.FormatUint(a.AssetID, 10), &info); err != nil {
			// A cancelled run is not an unresolved asset; stop and say so.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// One asset that will not resolve (deleted, algod hiccup) must not
			// cost the caller the balance and every other holding.
			unresolved++
			continue
		}
		if info.Params.Decimals > maxASADecimals {
			unresolved++
			continue
		}
		holding["decimals"] = info.Params.Decimals
		holding["unitName"] = info.Params.UnitName
		holding["name"] = info.Params.Name
		holding["amount"] = formatBaseUnits(a.Amount, info.Params.Decimals)
	}
	return map[string]any{
		"address":          body.Address,
		"algo":             formatBaseUnits(body.Amount, algoDecimals),
		"algoMicro":        body.Amount,
		"minBalance":       formatBaseUnits(body.MinBalance, algoDecimals),
		"minBalanceMicro":  body.MinBalance,
		"assets":           assets,
		"unresolvedAssets": unresolved,
	}, nil
}
