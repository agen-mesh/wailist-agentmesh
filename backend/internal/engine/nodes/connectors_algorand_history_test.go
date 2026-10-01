package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// indexerStub serves one canned /v2/accounts/{addr}/transactions body and
// answers asset lookups from algod's /v2/assets/{id}. Raw strings, so
// integers past 2^53 reach the connector exactly as the chain would send
// them. It records the last transactions query it was asked.
func indexerStub(t *testing.T, txns string, assets map[string]string) (*atomic.Int64, func() string) {
	t.Helper()
	var lookups atomic.Int64
	var lastQuery atomic.Value
	lastQuery.Store("")
	algod := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v2/assets/") {
			t.Errorf("unexpected algod path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		lookups.Add(1)
		body, ok := assets[strings.TrimPrefix(r.URL.Path, "/v2/assets/")]
		if !ok {
			http.Error(w, `{"message":"asset does not exist"}`, http.StatusNotFound)
			return
		}
		w.Write([]byte(body))
	}))
	idx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/transactions") {
			t.Errorf("unexpected indexer path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		lastQuery.Store(r.URL.RawQuery)
		w.Write([]byte(txns))
	}))
	t.Cleanup(algod.Close)
	t.Cleanup(idx.Close)
	SetAlgorandBases(algod.URL, idx.URL)
	t.Cleanup(func() { SetAlgorandBases("", "") })
	return &lookups, func() string { return lastQuery.Load().(string) }
}

func readTxns(t *testing.T, cfg map[string]string) map[string]any {
	t.Helper()
	if cfg == nil {
		cfg = map[string]string{}
	}
	if _, ok := cfg["algoAddress"]; !ok {
		cfg["algoAddress"] = "ABCDEF"
	}
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_transactions", Config: cfg}
	out, err := fetchAlgorandTransactions(context.Background(), node, emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchAlgorandTransactions: %v", err)
	}
	return out.(map[string]any)
}

const payAndTransfer = `{"transactions":[
 {"id":"TX1","tx-type":"pay","round-time":1757000000,"confirmed-round":48000000,"fee":1000,
  "sender":"SENDER1","payment-transaction":{"receiver":"RECV1","amount":9007199254740993}},
 {"id":"TX2","tx-type":"axfer","round-time":1757000600,"confirmed-round":48000100,"fee":1000,
  "sender":"SENDER2","asset-transfer-transaction":{"receiver":"RECV2","amount":1500000,"asset-id":31566704}}
],"next-token":"page2"}`

var usdcParams = map[string]string{
	"31566704": `{"index":31566704,"params":{"decimals":6,"unit-name":"USDC","name":"USDC","total":18446744073709551615,"creator":"CREATOR1","url":"https://centre.io"}}`,
}

// The whole point of this node: algod answers current state only, so history
// needs the indexer. Amounts must survive uint64 exactly, the same way the
// balance does -- 9007199254740993 is the first integer float64 cannot hold.
func TestAlgorandTransactionsKeepsAmountsExact(t *testing.T) {
	indexerStub(t, payAndTransfer, usdcParams)
	out := readTxns(t, nil)
	txs, _ := out["transactions"].([]map[string]any)
	if len(txs) != 2 {
		t.Fatalf("want 2 transactions, got %d (%+v)", len(txs), out)
	}
	if got := txs[0]["algoMicro"]; got != uint64(9007199254740993) {
		t.Errorf("algoMicro = %v, want the exact integer", got)
	}
	if got := txs[0]["algo"]; got != "9007199254.740993" {
		t.Errorf("algo = %v, want 9007199254.740993", got)
	}
	if got := txs[0]["type"]; got != "pay" {
		t.Errorf("type = %v", got)
	}
	if got := txs[0]["receiver"]; got != "RECV1" {
		t.Errorf("receiver = %v", got)
	}
}

// An asset transfer in base units reads as a wildly wrong token amount: 1.5
// USDC would be reported as 1,500,000 USDC. Same fix as the balance.
func TestAlgorandTransactionsDecimalAdjustsAssetTransfers(t *testing.T) {
	indexerStub(t, payAndTransfer, usdcParams)
	out := readTxns(t, nil)
	txs, _ := out["transactions"].([]map[string]any)
	tx := txs[1]
	if got := tx["amount"]; got != "1.5" {
		t.Errorf("amount = %v, want 1.5", got)
	}
	if got := tx["amountBaseUnits"]; got != uint64(1500000) {
		t.Errorf("amountBaseUnits = %v", got)
	}
	if got := tx["unitName"]; got != "USDC" {
		t.Errorf("unitName = %v", got)
	}
	if got := tx["assetId"]; got != uint64(31566704) {
		t.Errorf("assetId = %v", got)
	}
}

// round-time is a unix timestamp. An agent handed 1757000000 reports it as
// a number; handed an RFC3339 string it reports a date.
func TestAlgorandTransactionsFormatsTheTimestamp(t *testing.T) {
	indexerStub(t, payAndTransfer, usdcParams)
	out := readTxns(t, nil)
	txs, _ := out["transactions"].([]map[string]any)
	if got := txs[0]["time"]; got != "2025-09-04T15:33:20Z" {
		t.Errorf("time = %v, want an RFC3339 string", got)
	}
}

// Ten transfers of the same asset are one asset, not ten lookups.
func TestAlgorandTransactionsLooksUpEachAssetOnce(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"transactions":[`)
	for i := 0; i < 8; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"TX","tx-type":"axfer","round-time":1757000000,"confirmed-round":1,"fee":1000,
		 "sender":"S","asset-transfer-transaction":{"receiver":"R","amount":1000000,"asset-id":31566704}}`)
	}
	b.WriteString(`]}`)
	lookups, _ := indexerStub(t, b.String(), usdcParams)
	readTxns(t, nil)
	if got := lookups.Load(); got != 1 {
		t.Errorf("asset lookups = %d, want 1", got)
	}
}

// The limit is the user's, clamped to something an agent can actually read.
func TestAlgorandTransactionsClampsTheLimit(t *testing.T) {
	_, lastQuery := indexerStub(t, payAndTransfer, usdcParams)
	readTxns(t, map[string]string{"algoTxLimit": "9999"})
	if q := lastQuery(); !strings.Contains(q, "limit=50") {
		t.Errorf("query = %q, want the limit clamped to 50", q)
	}
	readTxns(t, map[string]string{"algoTxLimit": "0"})
	if q := lastQuery(); !strings.Contains(q, "limit=10") {
		t.Errorf("query = %q, want the default limit", q)
	}
}

func TestAlgorandTransactionsPassesTheTypeFilter(t *testing.T) {
	_, lastQuery := indexerStub(t, payAndTransfer, usdcParams)
	out := readTxns(t, map[string]string{"algoTxType": "AXFER"})
	if q := lastQuery(); !strings.Contains(q, "tx-type=axfer") {
		t.Errorf("query = %q, want the type filter passed through", q)
	}
	// The applied filter is echoed, so an agent can see what it asked for
	// rather than infer it from the rows.
	if got := out["type"]; got != "axfer" {
		t.Errorf("type = %v, want the applied filter echoed", got)
	}
}

// "payment" is the natural guess and the chain's code is "pay". Dropping an
// unknown filter returns every type, and the agent reports app calls and
// asset transfers as the payments it asked for. Forwarding it returns an
// empty page, which reads as "no payments". Both are wrong answers that look
// right, so the only honest response is to refuse and name the real codes.
func TestAlgorandTransactionsRefusesAnUnknownType(t *testing.T) {
	indexerStub(t, payAndTransfer, usdcParams)
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_transactions",
		Config: map[string]string{"algoAddress": "ABCDEF", "algoTxType": "payment"}}
	_, err := fetchAlgorandTransactions(context.Background(), node, emptyRunContext{})
	if err == nil {
		t.Fatal("an unknown transaction type must be refused, not ignored")
	}
	for _, want := range []string{"payment", "pay", "axfer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// No filter set is not an unknown filter.
func TestAlgorandTransactionsWithNoTypeReadsEveryType(t *testing.T) {
	_, lastQuery := indexerStub(t, payAndTransfer, usdcParams)
	out := readTxns(t, nil)
	if q := lastQuery(); strings.Contains(q, "tx-type=") {
		t.Errorf("query = %q, want no type filter", q)
	}
	if got := out["type"]; got != "all" {
		t.Errorf("type = %v, want all", got)
	}
}

// An address with no transactions is "none", not "unknown": a nil slice
// marshals to null and an agent handed null says it could not find out.
func TestAlgorandTransactionsReturnsAnEmptyListNotNull(t *testing.T) {
	indexerStub(t, `{"transactions":[]}`, nil)
	out := readTxns(t, nil)
	txs, ok := out["transactions"].([]map[string]any)
	if !ok || txs == nil {
		t.Fatalf("transactions = %#v, want an empty slice", out["transactions"])
	}
	if len(txs) != 0 {
		t.Errorf("want no transactions, got %d", len(txs))
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), `"transactions":null`) {
		t.Error("an empty history must not marshal as null")
	}
}

func TestAlgorandTransactionsFailsClosedWithoutAnIndexer(t *testing.T) {
	SetAlgorandBases("https://testnet-api.algonode.cloud", "")
	t.Cleanup(func() { SetAlgorandBases("", "") })
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_transactions",
		Config: map[string]string{"algoAddress": "ABCDEF"}}
	_, err := fetchAlgorandTransactions(context.Background(), node, emptyRunContext{})
	if err == nil || !strings.Contains(err.Error(), "ALGORAND_INDEXER_URL") {
		t.Fatalf("want an error naming the setting, got %v", err)
	}
}

// --- algorand_asset ---

func readAsset(t *testing.T, id string) map[string]any {
	t.Helper()
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_asset",
		Config: map[string]string{"algoAssetId": id}}
	out, err := fetchAlgorandAsset(context.Background(), node, emptyRunContext{})
	if err != nil {
		t.Fatalf("fetchAlgorandAsset: %v", err)
	}
	return out.(map[string]any)
}

// "What is ASA 31566704?" was only answerable for an asset you already held.
func TestAlgorandAssetDescribesATokenYouDoNotHold(t *testing.T) {
	algodStub(t, "{}", usdcParams)
	out := readAsset(t, "31566704")
	if out["unitName"] != "USDC" || out["name"] != "USDC" {
		t.Errorf("names = %v / %v", out["unitName"], out["name"])
	}
	if out["decimals"] != uint64(6) {
		t.Errorf("decimals = %v", out["decimals"])
	}
	if out["assetId"] != uint64(31566704) {
		t.Errorf("assetId = %v", out["assetId"])
	}
	if out["creator"] != "CREATOR1" {
		t.Errorf("creator = %v", out["creator"])
	}
}

// Total supply is the field most likely to exceed 2^53: USDC's is
// 18446744073709551615, the largest uint64 there is.
func TestAlgorandAssetKeepsTotalSupplyExact(t *testing.T) {
	algodStub(t, "{}", usdcParams)
	out := readAsset(t, "31566704")
	if got := out["totalBaseUnits"]; got != uint64(18446744073709551615) {
		t.Errorf("totalBaseUnits = %v, want the exact uint64", got)
	}
	if got := out["total"]; got != "18446744073709.551615" {
		t.Errorf("total = %v, want the decimal-adjusted string", got)
	}
}

func TestAlgorandAssetRejectsANonNumericId(t *testing.T) {
	algodStub(t, "{}", usdcParams)
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_asset",
		Config: map[string]string{"algoAssetId": "USDC"}}
	_, err := fetchAlgorandAsset(context.Background(), node, emptyRunContext{})
	if err == nil {
		t.Fatal("an asset id that is not a number must be refused, not sent to algod")
	}
	if !strings.Contains(err.Error(), "USDC") {
		t.Errorf("the error should name what it was given, got %v", err)
	}
}

func TestAlgorandAssetSkipsWithoutAnId(t *testing.T) {
	algodStub(t, "{}", usdcParams)
	node := models.WorkflowNode{Type: models.NodeTypeAction, Template: "algorand_asset"}
	_, err := fetchAlgorandAsset(context.Background(), node, emptyRunContext{})
	if err != ErrActionSkipped {
		t.Fatalf("want ErrActionSkipped, got %v", err)
	}
}

// A mainnet deployment that sets ALGORAND_NETWORK and ALGOD_URL but not the
// new ALGORAND_INDEXER_URL must not read mainnet balances beside testnet
// history. The default follows the declared network, the same way the relay's
// USDC asset id and CAIP-2 network do.
func TestAlgorandIndexerDefaultFollowsTheNetwork(t *testing.T) {
	cases := map[string]string{
		"mainnet": "https://mainnet-idx.algonode.cloud",
		"testnet": "https://testnet-idx.algonode.cloud",
		"":        "https://testnet-idx.algonode.cloud",
	}
	for network, want := range cases {
		if got := AlgorandIndexerDefault(network); got != want {
			t.Errorf("AlgorandIndexerDefault(%q) = %q, want %q", network, got, want)
		}
	}
}
