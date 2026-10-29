package purchasesim

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func testFixture(t *testing.T) (*Fixture, *time.Time) {
	t.Helper()
	now := time.Unix(1900000000, 0)
	f, err := New(Config{Resource: "https://merchant.example/tool", Payee: "0x2222222222222222222222222222222222222222", PriceMicros: "1000000", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return f, &now
}

func testKey(t *testing.T, number byte) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{number}, 32))
	if err != nil {
		t.Fatal("synthetic key construction")
	}
	return key
}

func testAuthorization(t *testing.T, f *Fixture, number byte) arc.Authorization {
	t.Helper()
	a, err := arc.SignAuthorization(testKey(t, 1), "USDC", "2", ArcChainID, common.HexToAddress(Asset), arc.Authorization{To: common.HexToAddress(f.config.Payee), Value: new(big.Int).Set(f.price), ValidAfter: big.NewInt(f.config.Now().Unix() - 10), ValidBefore: big.NewInt(f.config.Now().Unix() + 600), Nonce: common.BytesToHash(bytes.Repeat([]byte{number}, 32))})
	if err != nil {
		t.Fatal("synthetic authorization construction")
	}
	return a
}

func packAuthorization(t *testing.T, a arc.Authorization) []byte {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(`[{"type":"function","name":"transferWithAuthorization","inputs":[{"type":"address"},{"type":"address"},{"type":"uint256"},{"type":"uint256"},{"type":"uint256"},{"type":"bytes32"},{"type":"uint8"},{"type":"bytes32"},{"type":"bytes32"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := parsed.Pack("transferWithAuthorization", a.From, a.To, a.Value, a.ValidAfter, a.ValidBefore, a.Nonce, a.V, a.R, a.S)
	if err != nil {
		t.Fatal("authorization encoding")
	}
	return data
}

func signedTransaction(t *testing.T, data []byte, relayer byte, nonce uint64, mutate func(*types.DynamicFeeTx)) (string, common.Hash) {
	t.Helper()
	token := common.HexToAddress(Asset)
	tx := &types.DynamicFeeTx{ChainID: big.NewInt(ArcChainID), Nonce: nonce, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(1_000_000_000), Gas: 120000, To: &token, Value: big.NewInt(0), Data: data}
	if mutate != nil {
		mutate(tx)
	}
	signed, err := types.SignTx(types.NewTx(tx), types.LatestSignerForChainID(tx.ChainID), testKey(t, relayer))
	if err != nil {
		t.Fatal("synthetic transaction signing")
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatal("synthetic transaction encoding")
	}
	return hexutil.Encode(raw), signed.Hash()
}

type testResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func rpc(t *testing.T, h http.Handler, method string, params ...any) testResponse {
	t.Helper()
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "https://simulation.example", bytes.NewReader(data)))
	if w.Code != http.StatusOK {
		t.Fatalf("RPC HTTP status %d", w.Code)
	}
	var response testResponse
	if json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal("malformed RPC envelope")
	}
	return response
}

func result[T any](t *testing.T, r testResponse) T {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("RPC rejected request: %s", r.Error.Message)
	}
	var value T
	if err := json.Unmarshal(r.Result, &value); err != nil {
		t.Fatalf("decode RPC result: %v", err)
	}
	return value
}

func requireError(t *testing.T, response testResponse) {
	t.Helper()
	if response.Error == nil {
		t.Fatal("invalid request accepted")
	}
}

func setFaults(t *testing.T, f *Fixture, faults Faults) {
	t.Helper()
	if err := f.SetFaults(faults); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationAndRPCControls(t *testing.T) {
	f, _ := testFixture(t)
	for _, change := range []func(*Config){
		func(c *Config) { c.Resource = "http://merchant.example" },
		func(c *Config) { c.Resource = "https://user:secret@merchant.example" },
		func(c *Config) { c.Payee = "0x0000000000000000000000000000000000000000" },
		func(c *Config) { c.PriceMicros = "01" }, func(c *Config) { c.PriceMicros = "0" },
		func(c *Config) { c.PriceMicros = "18446744073709551616" },
	} {
		c := f.config
		change(&c)
		if _, err := New(c); err == nil {
			t.Fatal("bad configuration accepted")
		}
	}
	for _, body := range []string{`{`, `{"jsonrpc":"1.0","id":1,"method":"eth_chainId"}`, `{"jsonrpc":"2.0","id":1,"method":"eth_chainId"} {}`, `{"jsonrpc":"2.0","id":1,"method":"eth_chainId","extra":1}`} {
		w := httptest.NewRecorder()
		f.ArcHandler().ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		var r testResponse
		if json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Error == nil {
			t.Fatal("bad envelope accepted")
		}
	}
	w := httptest.NewRecorder()
	f.ArcHandler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 405 {
		t.Fatal("GET accepted")
	}
	w = httptest.NewRecorder()
	f.ArcHandler().ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 1024*1024+1))))
	if w.Code != 400 {
		t.Fatal("oversized request accepted")
	}
	requireError(t, rpc(t, f.ArcHandler(), "unsupported"))
	if f.SetFaults(Faults{RPC: "unsupported"}) == nil {
		t.Fatal("unsupported fault accepted")
	}
	capabilities := f.Capabilities()
	if capabilities.EvidenceMode != "simulation" || capabilities.Features["arkiv.write"] || capabilities.Features["merchant.execute"] || !capabilities.Features["rpc.wrong_transfer"] {
		t.Fatal("capability disclosure mismatch")
	}
}

func TestPrivateStateSurvivesFreshInstance(t *testing.T) {
	f, _ := testFixture(t)
	a := testAuthorization(t, f, 1)
	raw, hash := signedTransaction(t, packAuthorization(t, a), 2, 0, nil)
	result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw))
	setFaults(t, f, Faults{RPC: "pending", Arkiv: "changed_version"})
	b := testAuthorization(t, f, 2)
	pending, pendingHash := signedTransaction(t, packAuthorization(t, b), 2, 1, nil)
	result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", pending))
	data, err := f.MarshalState()
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err = g.RestoreState(data); err != nil {
		t.Fatal(err)
	}
	if got := result[*types.Receipt](t, rpc(t, g.ArcHandler(), "eth_getTransactionReceipt", hash.Hex())); got == nil || got.TxHash != hash || len(got.Logs) != 2 {
		t.Fatal("confirmed payment lost on restore")
	}
	if got := result[*types.Receipt](t, rpc(t, g.ArcHandler(), "eth_getTransactionReceipt", pendingHash.Hex())); got != nil {
		t.Fatal("pending state lost on restore")
	}
	if g.Observations().UniqueTransactions != 2 || g.Observations().ConsumedAuthorizations != 1 {
		t.Fatal("restore counters mismatch")
	}
	setFaults(t, g, Faults{})
	if got := result[*types.Receipt](t, rpc(t, g.ArcHandler(), "eth_getTransactionReceipt", pendingHash.Hex())); got == nil || got.Status != 1 {
		t.Fatal("pending payment did not settle after restore")
	}
	if got := result[string](t, rpc(t, g.ArcHandler(), "eth_sendRawTransaction", raw)); got != hash.Hex() || g.Observations().UniqueTransactions != 2 {
		t.Fatal("restore replay changed payment")
	}
	for _, bad := range [][]byte{[]byte(`{}`), append(append([]byte{}, data...), byte('x')), bytes.Replace(data, []byte(`"Version":1`), []byte(`"Version":2`), 1)} {
		if g.RestoreState(bad) == nil {
			t.Fatal("corrupt state accepted")
		}
	}
	other := f.config
	other.PriceMicros = "2"
	h, err := New(other)
	if err != nil {
		t.Fatal(err)
	}
	if h.RestoreState(data) == nil {
		t.Fatal("cross-configuration restore accepted")
	}
	obs := g.Observations()
	obs.RPCCalls["secret"] = 9
	obs.TransactionHashes = append(obs.TransactionHashes, "wrong")
	if g.Observations().RPCCalls["secret"] != 0 {
		t.Fatal("observations share mutable counters")
	}
	public, err := json.Marshal(g.Observations())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(public, []byte(raw)) || bytes.Contains(public, []byte(`"signature"`)) || bytes.Contains(public, []byte(`"Tx"`)) {
		t.Fatal("public observations expose transaction material")
	}
	snapshot := g.Snapshot()
	g.Reset()
	if g.Observations().UniqueTransactions != 0 {
		t.Fatal("explicit reset did not clear chain")
	}
	if err := g.Restore(snapshot); err != nil || g.Observations().UniqueTransactions != 2 {
		t.Fatal("in-process snapshot failed")
	}
}
