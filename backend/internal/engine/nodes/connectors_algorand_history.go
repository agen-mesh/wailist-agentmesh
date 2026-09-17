package nodes

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// Transaction history and standalone asset lookup. Both are questions
// algorand_account cannot answer: algod holds current state only, and its
// asset lookup was reachable only through an account that already held the
// asset. "What did this address do" and "what is this token" are the two
// things anyone asks about Algorand first, and neither had a first-party
// node -- the builder had to buy an answer from the x402 Bazaar, where the
// endpoint might be dead and the shape of its reply is whatever it is.

// Transaction-page sizes. defaultTxLimit is what an agent can hold in a
// prompt and still reason about; maxTxLimit is the ceiling, because the
// result is read by a model, not paged through by a person, and a
// thousand-row history is tokens spent to no purpose.
const (
	defaultTxLimit = 10
	maxTxLimit     = 50
)

// algorandTxTypes are the transaction types the chain has. A filter is only
// forwarded if it is one of these: an unknown value passed straight through
// makes the indexer return an empty page, which reads as "this address has
// no transactions" -- a wrong answer that looks like a right one.
var algorandTxTypes = map[string]bool{
	"pay": true, "keyreg": true, "acfg": true, "axfer": true,
	"afrz": true, "appl": true, "stpf": true, "hb": true,
}

// indexerTransaction is the part of an indexer transaction this connector
// reads. Amounts and rounds are uint64 and stay uint64: see getAlgodJSON.
type indexerTransaction struct {
	ID             string `json:"id"`
	Type           string `json:"tx-type"`
	RoundTime      int64  `json:"round-time"`
	ConfirmedRound uint64 `json:"confirmed-round"`
	Fee            uint64 `json:"fee"`
	Sender         string `json:"sender"`
	Payment        *struct {
		Receiver string `json:"receiver"`
		Amount   uint64 `json:"amount"`
	} `json:"payment-transaction"`
	AssetTransfer *struct {
		Receiver string `json:"receiver"`
		Amount   uint64 `json:"amount"`
		AssetID  uint64 `json:"asset-id"`
	} `json:"asset-transfer-transaction"`
}

// assetResolver looks up an ASA's parameters at most once per run, however
// many transactions mention it. A history of twenty USDC transfers is one
// asset, and twenty identical round trips to algod would be the slowest part
// of the node by far.
type assetResolver struct {
	seen  map[uint64]*algorandAssetParams
	calls int
}

func newAssetResolver() *assetResolver {
	return &assetResolver{seen: map[uint64]*algorandAssetParams{}}
}

// get returns an asset's parameters, or nil when they could not be read.
// A nil result is not an error: one asset that will not resolve (deleted,
// algod hiccup, past the lookup cap) must not cost the caller the whole
// history, so the amount is still reported in base units and counted as
// unresolved.
func (a *assetResolver) get(ctx context.Context, id uint64) *algorandAssetParams {
	if info, ok := a.seen[id]; ok {
		return info
	}
	if a.calls >= maxAlgorandAssetLookups {
		return nil
	}
	a.calls++
	var info algorandAssetParams
	if err := getAlgodJSON(ctx, "/v2/assets/"+strconv.FormatUint(id, 10), &info); err != nil {
		a.seen[id] = nil
		return nil
	}
	if info.Params.Decimals > maxASADecimals {
		a.seen[id] = nil
		return nil
	}
	a.seen[id] = &info
	return &info
}

// fetchAlgorandTransactions reads an address's recent transaction history
// from the indexer.
//
// Flat, one map per transaction, for the same reason fetchAlgorandAccount is
// flat: the indexer nests a payment's amount under
// payment-transaction.amount and an asset transfer's under
// asset-transfer-transaction.amount, so the field a builder has to name
// depends on the type of a row it has not seen yet. Here both land on
// `receiver` and on an amount pair, and the row says which it is in `type`.
//
// Amounts keep the same exact-integer discipline as the balance: an exact
// decimal string for a person to read, the raw uint64 beside it, and never a
// float in between.
func fetchAlgorandTransactions(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	address := strings.TrimSpace(resolveTemplate(configVal(node, "algoAddress", ""), rc))
	if address == "" {
		return "algorand_skipped_no_address", ErrActionSkipped
	}
	if indexerAPIBase == "" {
		return nil, fmt.Errorf("algorand: no Algorand indexer is configured -- set ALGORAND_INDEXER_URL")
	}

	limit := defaultTxLimit
	if n, err := strconv.Atoi(strings.TrimSpace(resolveTemplate(configVal(node, "algoTxLimit", ""), rc))); err == nil && n > 0 {
		limit = min(n, maxTxLimit)
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if t := strings.ToLower(strings.TrimSpace(resolveTemplate(configVal(node, "algoTxType", ""), rc))); algorandTxTypes[t] {
		query.Set("tx-type", t)
	}

	var body struct {
		Transactions []indexerTransaction `json:"transactions"`
	}
	path := "/v2/accounts/" + url.PathEscape(address) + "/transactions?" + query.Encode()
	if err := getIndexerJSON(ctx, path, &body); err != nil {
		return nil, err
	}

	assets := newAssetResolver()
	// Non-nil even when empty: nil marshals to null, and an agent handed
	// null says "unknown" where the true answer is "this address has none".
	out := make([]map[string]any, 0, len(body.Transactions))
	unresolved := 0
	for _, tx := range body.Transactions {
		row := map[string]any{
			"id":        tx.ID,
			"type":      tx.Type,
			"round":     tx.ConfirmedRound,
			"sender":    tx.Sender,
			"fee":       formatBaseUnits(tx.Fee, algoDecimals),
			"feeMicro":  tx.Fee,
			"direction": txDirection(tx, address),
		}
		if tx.RoundTime > 0 {
			// round-time is unix seconds. An agent handed 1757000000
			// reports it as a number; handed a date it reports a date.
			row["time"] = time.Unix(tx.RoundTime, 0).UTC().Format(time.RFC3339)
		}
		switch {
		case tx.Payment != nil:
			row["receiver"] = tx.Payment.Receiver
			row["algo"] = formatBaseUnits(tx.Payment.Amount, algoDecimals)
			row["algoMicro"] = tx.Payment.Amount
		case tx.AssetTransfer != nil:
			row["receiver"] = tx.AssetTransfer.Receiver
			row["assetId"] = tx.AssetTransfer.AssetID
			row["amountBaseUnits"] = tx.AssetTransfer.Amount
			if info := assets.get(ctx, tx.AssetTransfer.AssetID); info != nil {
				row["decimals"] = info.Params.Decimals
				row["unitName"] = info.Params.UnitName
				row["name"] = info.Params.Name
				row["amount"] = formatBaseUnits(tx.AssetTransfer.Amount, info.Params.Decimals)
			} else {
				// A cancelled run is not an unresolved asset.
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				unresolved++
			}
		}
		out = append(out, row)
	}
	return map[string]any{
		"address":          address,
		"count":            len(out),
		"transactions":     out,
		"unresolvedAssets": unresolved,
	}, nil
}

// txDirection says whether this address sent or received the transaction,
// which is the first thing anyone asks of a history and the one thing a row
// of addresses does not make obvious.
func txDirection(tx indexerTransaction, address string) string {
	if tx.Sender == address {
		return "out"
	}
	switch {
	case tx.Payment != nil && tx.Payment.Receiver == address:
		return "in"
	case tx.AssetTransfer != nil && tx.AssetTransfer.Receiver == address:
		return "in"
	}
	return "other"
}

// fetchAlgorandAsset describes one ASA by id.
//
// algorand_account already reads /v2/assets/{id}, but only for an asset the
// account being read already holds, so "what is ASA 31566704" was
// unanswerable for every other token on the chain.
func fetchAlgorandAsset(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	raw := strings.TrimSpace(resolveTemplate(configVal(node, "algoAssetId", ""), rc))
	if raw == "" {
		return "algorand_skipped_no_asset_id", ErrActionSkipped
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		// Caught here rather than at algod, which would answer 404 and make
		// a wrong input look like a missing asset. An ASA is identified by
		// a number; a ticker is not an id.
		return nil, fmt.Errorf("algorand: %q is not an Algorand asset id -- an id is a number, such as 31566704 for USDC", raw)
	}
	if algodAPIBase == "" {
		return nil, fmt.Errorf("algorand: no Algorand node is configured -- set ALGOD_URL")
	}
	var body struct {
		Index  uint64 `json:"index"`
		Params struct {
			Decimals      uint64 `json:"decimals"`
			UnitName      string `json:"unit-name"`
			Name          string `json:"name"`
			Total         uint64 `json:"total"`
			URL           string `json:"url"`
			Creator       string `json:"creator"`
			Manager       string `json:"manager"`
			Reserve       string `json:"reserve"`
			Freeze        string `json:"freeze"`
			Clawback      string `json:"clawback"`
			DefaultFrozen bool   `json:"default-frozen"`
		} `json:"params"`
	}
	if err := getAlgodJSON(ctx, "/v2/assets/"+strconv.FormatUint(id, 10), &body); err != nil {
		return nil, err
	}
	p := body.Params
	if p.Decimals > maxASADecimals {
		return nil, fmt.Errorf("algorand: asset %d reports %d decimals, which is not a real asset", id, p.Decimals)
	}
	return map[string]any{
		"assetId":  id,
		"name":     p.Name,
		"unitName": p.UnitName,
		"decimals": p.Decimals,
		// Total supply is the field most likely to pass 2^53: USDC's is the
		// largest uint64 there is.
		"total":          formatBaseUnits(p.Total, p.Decimals),
		"totalBaseUnits": p.Total,
		"url":            p.URL,
		"creator":        p.Creator,
		"manager":        p.Manager,
		"reserve":        p.Reserve,
		"freeze":         p.Freeze,
		"clawback":       p.Clawback,
		"defaultFrozen":  p.DefaultFrozen,
	}, nil
}
