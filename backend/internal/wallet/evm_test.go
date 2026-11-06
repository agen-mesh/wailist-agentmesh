package wallet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/agentmesh/backend/internal/chain"
	"github.com/agentmesh/backend/internal/wallet"
	"github.com/agentmesh/backend/internal/x402"
)

const testEncKey = "test-enc-key-32-bytes-long-12345"

// Golden domain separator read from the live token's DOMAIN_SEPARATOR() on
// 2026-11-06. If chain.TokenName/TokenVersion drift, every real signature
// would fail; this catches it offline.
const liveDomainSeparator = "0xcdcfacb6b15c8238b4e5fbb47bdb83b7f83f50a531d264bf9763d2bd1d3d2a25"

func TestValidateNetwork(t *testing.T) {
	var tests = []struct {
		name     string
		response string
		wantErr  string
	}{
		{name: "Whitechain testnet", response: `{"jsonrpc":"2.0","id":1,"result":"0x752"}`},
		{name: "Whitechain mainnet", response: `{"jsonrpc":"2.0","id":1,"result":"0x753"}`, wantErr: "unsupported RPC chain ID 1875"},
		{name: "Ethereum mainnet", response: `{"jsonrpc":"2.0","id":1,"result":"0x1"}`, wantErr: "unsupported RPC chain ID 1"},
		{name: "zero chain", response: `{"jsonrpc":"2.0","id":1,"result":"0x0"}`, wantErr: "unsupported RPC chain ID 0"},
		{name: "malformed chain", response: `{"jsonrpc":"2.0","id":1,"result":"testnet"}`, wantErr: "decode RPC chain ID"},
		{name: "missing result", response: `{"jsonrpc":"2.0","id":1}`, wantErr: "check Whitechain testnet RPC"},
		{name: "RPC error", response: `{"jsonrpc":"2.0","id":1,"error":{"message":"unavailable"}}`, wantErr: "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Method string `json:"method"`
				}
				decodeErr := json.NewDecoder(r.Body).Decode(&request)
				if decodeErr != nil {
					t.Errorf("decode chain check request: %v", decodeErr)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if request.Method != "eth_chainId" {
					t.Errorf("RPC method = %q, want eth_chainId", request.Method)
				}
				_, writeErr := w.Write([]byte(tt.response))
				if writeErr != nil {
					t.Errorf("write chain check response: %v", writeErr)
				}
			}))
			defer server.Close()
			svc := wallet.NewService(testEncKey, server.URL)
			err := svc.ValidateNetwork(context.Background())
			if tt.wantErr == "" && err != nil {
				t.Fatalf("ValidateNetwork: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("ValidateNetwork error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestDomainSeparatorMatchesChain(t *testing.T) {
	td := wallet.TypedDataForTest(wallet.Domain{Name: chain.TokenName, Version: chain.TokenVersion, ChainID: chain.ChainID, VerifyingContract: chain.USDTAddress})
	sep, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		t.Fatal(err)
	}
	if got := hexutil.Encode(sep); got != liveDomainSeparator {
		t.Fatalf("domain separator = %s, want %s", got, liveDomainSeparator)
	}
}

func TestSignAuthorizationRecovers(t *testing.T) {
	pk, _ := crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318")
	from := crypto.PubkeyToAddress(pk.PublicKey).Hex()
	a := x402.Authorization{From: from, To: "0x26B70253feE141dEE4ece1E5efBe2cf70EAe26fc", Value: "10000", ValidAfter: "0", ValidBefore: "1900000000", Nonce: "0x" + "11" + "00000000000000000000000000000000000000000000000000000000000000"}
	d := wallet.Domain{Name: chain.TokenName, Version: chain.TokenVersion, ChainID: chain.ChainID, VerifyingContract: chain.USDTAddress}
	sig, err := wallet.SignAuthorization(pk, d, a)
	if err != nil {
		t.Fatal(err)
	}
	// Produced independently by `cast wallet sign --data` (Foundry) for the
	// same typed data; byte-identical output pins the EIP-712 encoding.
	const castSig = "0xfe891cdb94e36e054e4d8c08cdd3a7a8da275510d29bde3c551b5cd964eb09237f83b9917fd09f931a8a88685cad494a87fddb1cc34f505b26af2c1e088261a91b"
	if sig != castSig {
		t.Fatalf("signature %s != cast golden %s", sig, castSig)
	}
	raw, _ := hexutil.Decode(sig)
	if len(raw) != 65 || (raw[64] != 27 && raw[64] != 28) {
		t.Fatalf("bad signature shape %s", sig)
	}
	got, err := wallet.RecoverAuthorizationSigner(d, a, sig)
	if err != nil || got != from {
		t.Fatalf("recovered %s (%v), want %s", got, err, from)
	}
}

func TestServiceSignTransferAuthorization(t *testing.T) {
	svc := wallet.NewService(testEncKey, "")
	addr, enc, err := svc.GenerateWallet()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.AddressForEncKey(enc); got != addr {
		t.Fatalf("AddressForEncKey = %s, want %s", got, addr)
	}
	p, err := svc.SignTransferAuthorization(context.Background(), enc, wallet.AuthorizationRequest{
		Asset: chain.USDTAddress, PayTo: "0x26b70253fee141dee4ece1e5efbe2cf70eae26fc", Amount: 10000, Extra: chain.Extra(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Authorization.From != addr || p.Authorization.Value != "10000" || len(p.Authorization.Nonce) != 66 {
		t.Fatalf("unexpected authorization %+v", p.Authorization)
	}
	d := wallet.Domain{Name: chain.TokenName, Version: chain.TokenVersion, ChainID: chain.ChainID, VerifyingContract: chain.USDTAddress}
	if got, _ := wallet.RecoverAuthorizationSigner(d, p.Authorization, p.Signature); got != addr {
		t.Fatalf("signature recovers to %s, want %s", got, addr)
	}
}

func TestServiceRefusesOtherAssets(t *testing.T) {
	svc := wallet.NewService(testEncKey, "")
	_, enc, _ := svc.GenerateWallet()
	_, err := svc.SignTransferAuthorization(context.Background(), enc, wallet.AuthorizationRequest{
		Asset: "0x5Eb541ba8A2DD841aF7864c3389162a23ddA8401", PayTo: "0x26B70253feE141dEE4ece1E5efBe2cf70EAe26fc", Amount: 1,
	})
	if err == nil {
		t.Fatal("expected refusal for a non-allowlisted asset")
	}
}

func TestEncryptPrivateKeyRoundTrip(t *testing.T) {
	svc := wallet.NewService(testEncKey, "")
	addr, enc, err := svc.EncryptPrivateKey("0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.AddressForEncKey(enc); got != addr {
		t.Fatalf("round trip %s != %s", got, addr)
	}
}
