package arc

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Public Hardhat/Anvil account 0. This key is published for local chains.
const testKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func testCfg() Config {
	key, err := crypto.HexToECDSA(testKey)
	if err != nil {
		panic(err)
	}
	return Config{
		Network:   Testnet,
		MaxMicros: big.NewInt(1_000_000_000),
		Key:       key,
		Address:   crypto.PubkeyToAddress(key.PublicKey),
		PayTo:     crypto.PubkeyToAddress(key.PublicKey).Hex(),
	}
}

func TestSignAuthorizationRecoversTheSigner(t *testing.T) {
	cfg := testCfg()
	token := common.HexToAddress(ArcUSDC)
	to := common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	// The blocklist check lives in Prepare. Signing itself must still be
	// able to recover a signature made to any address, including this one,
	// so the check cannot be skipped by using a different helper.
	_ = to
	auth := Authorization{
		To:          common.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"),
		Value:       big.NewInt(10_000),
		ValidAfter:  big.NewInt(0),
		ValidBefore: big.NewInt(9_999_999_999),
		Nonce:       common.HexToHash("0x01"),
	}
	signed, err := SignAuthorization(cfg.Key, "USDC", "2", 5042002, token, auth)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RecoverAuthorizationSigner("USDC", "2", 5042002, token, signed)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg.Address {
		t.Fatalf("recovered %s, key is %s", got.Hex(), cfg.Address.Hex())
	}
	if signed.V != 27 && signed.V != 28 {
		t.Fatalf("v = %d, USDC expects 27 or 28", signed.V)
	}
}

func TestPrepareRejectsTheCasesThatWouldMoveTheWrongMoney(t *testing.T) {
	cfg := Config{Network: Testnet, MaxMicros: big.NewInt(1_000_000_000)}
	okTo := "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"

	if _, err := Prepare(cfg, Request{Op: "transfer", ChainID: "ethereum", To: okTo, Amount: "1"}); err == nil || !strings.Contains(err.Error(), "ARC_NETWORK") {
		t.Fatalf("mainnet chain while testnet: %v", err)
	}
	if _, err := Prepare(cfg, Request{Op: "transfer", ChainID: "arc-testnet", To: Blocklisted, Amount: "1"}); err == nil || !strings.Contains(err.Error(), "blocklisted") {
		t.Fatalf("blocklist: %v", err)
	}
	if _, err := Prepare(cfg, Request{Op: "transfer", ChainID: "arc-testnet", To: okTo, Amount: "1.2345678"}); err == nil {
		t.Fatal("7 fractional digits were accepted")
	}
	if _, err := Prepare(cfg, Request{Op: "transfer", ChainID: "arc-testnet", To: okTo, Amount: "1001"}); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("cap: %v", err)
	}
	if _, err := Prepare(cfg, Request{Op: "bridge", ChainID: "algorand-testnet", DestChainID: "arc-testnet", To: okTo, Amount: "1"}); err == nil || !strings.Contains(err.Error(), "not a CCTP domain") {
		t.Fatalf("algorand bridge: %v", err)
	}
	quoted, err := Prepare(cfg, Request{Op: "nanopay", ChainID: "arc-testnet", To: okTo, Amount: "0.01"})
	if err != nil {
		t.Fatal(err)
	}
	res := quoted.quote("default")
	if res.Status != "quoted" || res.Micros != "10000" {
		t.Fatalf("quote: %+v", res)
	}
	// Dropping a node must not imply a broadcast. Execute with Broadcast
	// false returns before any RPC, so a nil caller is safe.
	got, err := Execute(context.Background(), nil, cfg, Request{Op: "nanopay", ChainID: "arc-testnet", To: okTo, Amount: "0.01"})
	if err != nil || got.Status != "quoted" {
		t.Fatalf("broadcast default: %+v %v", got, err)
	}
}

func TestDepositForBurnSelectorMatchesTheCCTPSignature(t *testing.T) {
	data, err := packDepositForBurn(
		big.NewInt(1_000_000),
		6,
		common.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"),
		common.HexToAddress(ArcUSDC),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := crypto.Keccak256([]byte("depositForBurn(uint256,uint32,bytes32,address,bytes32,uint256,uint32)"))[:4]
	if string(data[:4]) != string(want) {
		t.Fatalf("selector %x, want %x", data[:4], want)
	}
	if data[4+32+32+31] != 0 && len(data) < 4+32*7 {
		t.Fatal("calldata shorter than the seven words CCTP expects")
	}
}

type fakeCaller struct {
	usdc *big.Int
	eurc *big.Int
}

func (f fakeCaller) Call(_ context.Context, _ string, method string, params []any, out any) error {
	if method != "eth_call" {
		return errAmount
	}
	call := params[0].(map[string]string)
	token := strings.ToLower(call["to"])
	bal := big.NewInt(0)
	arc, _ := ChainByID(Testnet, "arc-testnet")
	switch token {
	case strings.ToLower(arc.USDC):
		bal = f.usdc
	case strings.ToLower(arc.EURC):
		bal = f.eurc
	}
	if bal == nil {
		bal = big.NewInt(0)
	}
	raw := make([]byte, 32)
	bal.FillBytes(raw)
	encoded, _ := json.Marshal(encodeHex(raw))
	return json.Unmarshal(encoded, out)
}

func TestTreasurySumsERC20USDCOnly(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/balances" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balances":[{"domain":26,"balance":"5"}]}`))
	}))
	defer gateway.Close()

	cfg := Config{Network: Testnet, GatewayURL: gateway.URL, MaxMicros: big.NewInt(1)}
	// 1.5 USDC as ERC-20 units, and 2 EURC. A native 18-decimal balance is
	// not served by the fake; asking for one would error the call.
	fake := fakeCaller{usdc: big.NewInt(1_500_000), eurc: big.NewInt(2_000_000)}
	got, err := ReadTreasury(context.Background(), fake, gateway.Client(), cfg,
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.WalletUSDC != "1.5" {
		t.Fatalf("wallet USDC %s, want 1.5 (EURC and Gateway must stay out of this sum)", got.WalletUSDC)
	}
	if got.GatewayUSDC != "5" {
		t.Fatalf("gateway %s", got.GatewayUSDC)
	}
	var sawEURC bool
	for _, row := range got.Balances {
		if row.Symbol == "EURC" && row.ChainID == "arc-testnet" {
			sawEURC = true
			if row.IncludedInUSDC {
				t.Fatal("EURC was marked as part of the USDC sum")
			}
			if row.Amount != "2" {
				t.Fatalf("eurc amount %s", row.Amount)
			}
		}
	}
	if !sawEURC {
		t.Fatal("Arc EURC balance missing")
	}
	if got.Partial {
		t.Fatalf("partial treasury: %+v", got.Balances)
	}
}

func TestParseGatewayDecimalIsNotBaseUnits(t *testing.T) {
	lines, total, err := ParseGatewayBalances([]byte(`{"token":"USDC","balances":[{"domain":26,"balance":"3.311999","pendingBatch":"0"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if total.String() != "3311999" {
		t.Fatalf("total = %s", total)
	}
	if len(lines) != 1 || lines[0].Amount != "3.311999" || lines[0].IncludedInUSDC {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestParseIrisPendingAndAlgoHolding(t *testing.T) {
	_, _, status, err := ParseIris([]byte(`{"messages":[{"attestation":"PENDING","status":"pending"}]}`))
	if err != nil || status != "pending" {
		t.Fatalf("pending: %s %v", status, err)
	}
	msg, att, status, err := ParseIris([]byte(`{"messages":[{"message":"0x01","attestation":"0x02","status":"complete"}]}`))
	if err != nil || status != "complete" || msg != "0x01" || att != "0x02" {
		t.Fatalf("complete: %s %s %s %v", status, msg, att, err)
	}
	micros, err := ParseAlgoUSDC([]byte(`{"account":{"assets":[{"asset-id":10458941,"amount":2500000}]}}`), 10458941)
	if err != nil || micros.String() != "2500000" {
		t.Fatalf("algo: %v %v", micros, err)
	}
	// A different ASA must not be reported as USDC.
	other, err := ParseAlgoUSDC([]byte(`{"account":{"assets":[{"asset-id":1,"amount":99}]}}`), 10458941)
	if err != nil || other.Sign() != 0 {
		t.Fatalf("other asset: %v %v", other, err)
	}
}
