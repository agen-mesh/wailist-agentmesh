package wallet

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"

	"github.com/agentmesh/backend/internal/chain"
	"github.com/agentmesh/backend/internal/x402"
)

// Service owns AgentMesh's Whitechain (EVM) keys. A key is stored as its
// 0x-prefixed hex private key, AES-GCM encrypted with ENCRYPTION_KEY (see
// crypto.go); the plaintext never leaves this package.
//
// Wallets never broadcast transactions for x402: they only sign EIP-3009
// TransferWithAuthorization messages, and the facilitator's relayer submits
// them and pays the WBT gas. The RPC URL is used for read-only balance calls.
type Service struct {
	encKey string
	rpcURL string
	http   *http.Client
}

func NewService(encKey, rpcURL string) *Service {
	if rpcURL == "" {
		rpcURL = chain.DefaultRPCURL
	}
	return &Service{encKey: encKey, rpcURL: rpcURL, http: &http.Client{Timeout: 15 * time.Second}}
}

// Network is the CAIP-2 network every signature from this service targets.
func (s *Service) Network() string { return chain.Network }

func (s *Service) ValidateNetwork(ctx context.Context) error {
	var result string
	if err := s.rpc(ctx, "eth_chainId", []any{}, &result); err != nil {
		return fmt.Errorf("check Whitechain testnet RPC: %w", err)
	}
	id, err := hexutil.DecodeBig(result)
	if err != nil {
		return fmt.Errorf("decode RPC chain ID: %w", err)
	}
	if id.Cmp(big.NewInt(chain.ChainID)) != 0 {
		return fmt.Errorf("unsupported RPC chain ID %s: this build settles on %s only (%d)", id, chain.Name, chain.ChainID)
	}
	return nil
}

// GenerateWallet creates a fresh secp256k1 key and returns its checksummed
// address plus the encrypted private key.
func (s *Service) GenerateWallet() (address, encKey string, err error) {
	pk, err := crypto.GenerateKey()
	if err != nil {
		return "", "", err
	}
	enc, err := Encrypt(hexutil.Encode(crypto.FromECDSA(pk)), s.encKey)
	if err != nil {
		return "", "", err
	}
	return crypto.PubkeyToAddress(pk.PublicKey).Hex(), enc, nil
}

// EncryptPrivateKey validates a hex private key and encrypts it for storage.
func (s *Service) EncryptPrivateKey(hexKey string) (address, encKey string, err error) {
	pk, err := parsePrivateKey(hexKey)
	if err != nil {
		return "", "", err
	}
	enc, err := Encrypt(hexutil.Encode(crypto.FromECDSA(pk)), s.encKey)
	if err != nil {
		return "", "", err
	}
	return crypto.PubkeyToAddress(pk.PublicKey).Hex(), enc, nil
}

func (s *Service) privateKey(encKey string) (*ecdsa.PrivateKey, error) {
	plain, err := Decrypt(encKey, s.encKey)
	if err != nil {
		return nil, err
	}
	return parsePrivateKey(plain)
}

func parsePrivateKey(hexKey string) (*ecdsa.PrivateKey, error) {
	pk, err := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(hexKey), "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	return pk, nil
}

// AddressForEncKey decrypts encKey and derives the address it controls. Used
// at boot to check every PLATFORM_*_ADDRESS env var against its key, so a
// mispaired address/key fails loudly instead of at payment time.
func (s *Service) AddressForEncKey(encKey string) (string, error) {
	pk, err := s.privateKey(encKey)
	if err != nil {
		return "", err
	}
	return crypto.PubkeyToAddress(pk.PublicKey).Hex(), nil
}

// AuthorizationRequest is what a caller wants paid.
type AuthorizationRequest struct {
	// Asset is the token (EIP-712 verifyingContract). Must be the allowlisted
	// USDT on chain.ChainID.
	Asset string
	PayTo string
	// Amount in token base units (6 decimals).
	Amount uint64
	// Extra is the 402's accepts[].extra; its name/version override the
	// default domain only if both are present.
	Extra map[string]any
	// ValidFor bounds validBefore. Zero means 5 minutes.
	ValidFor time.Duration
}

// SignTransferAuthorization signs an EIP-3009 TransferWithAuthorization
// from the key in encKey and returns the x402 exact-EVM payload.
func (s *Service) SignTransferAuthorization(_ context.Context, encKey string, r AuthorizationRequest) (x402.ExactEVMPayload, error) {
	if !chain.IsUSDT(r.Asset) {
		return x402.ExactEVMPayload{}, fmt.Errorf("refusing to sign for asset %q: only %s %s on %s is supported", r.Asset, chain.TokenSymbol, chain.USDTAddress, chain.Network)
	}
	if !chain.IsAddress(r.PayTo) {
		return x402.ExactEVMPayload{}, fmt.Errorf("invalid payTo address %q", r.PayTo)
	}
	if r.Amount == 0 {
		return x402.ExactEVMPayload{}, errors.New("amount must be positive")
	}
	pk, err := s.privateKey(encKey)
	if err != nil {
		return x402.ExactEVMPayload{}, err
	}
	validFor := r.ValidFor
	if validFor <= 0 {
		validFor = 5 * time.Minute
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return x402.ExactEVMPayload{}, fmt.Errorf("generate nonce: %w", err)
	}
	name, version := chain.TokenName, chain.TokenVersion
	if r.Extra != nil {
		n, _ := r.Extra["name"].(string)
		v, _ := r.Extra["version"].(string)
		if n != "" && v != "" {
			name, version = n, v
		}
	}
	auth := x402.Authorization{
		From:        crypto.PubkeyToAddress(pk.PublicKey).Hex(),
		To:          common.HexToAddress(r.PayTo).Hex(),
		Value:       new(big.Int).SetUint64(r.Amount).String(),
		ValidAfter:  "0",
		ValidBefore: fmt.Sprint(time.Now().Add(validFor).Unix()),
		Nonce:       hexutil.Encode(nonce),
	}
	sig, err := SignAuthorization(pk, Domain{Name: name, Version: version, ChainID: chain.ChainID, VerifyingContract: r.Asset}, auth)
	if err != nil {
		return x402.ExactEVMPayload{}, err
	}
	return x402.ExactEVMPayload{Signature: sig, Authorization: auth}, nil
}

// Domain is an EIP-712 token domain.
type Domain struct {
	Name              string
	Version           string
	ChainID           int64
	VerifyingContract string
}

func typedData(d Domain, a x402.Authorization) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TransferWithAuthorization": {
				{Name: "from", Type: "address"},
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "validAfter", Type: "uint256"},
				{Name: "validBefore", Type: "uint256"},
				{Name: "nonce", Type: "bytes32"},
			},
		},
		PrimaryType: "TransferWithAuthorization",
		Domain: apitypes.TypedDataDomain{
			Name:              d.Name,
			Version:           d.Version,
			ChainId:           math.NewHexOrDecimal256(d.ChainID),
			VerifyingContract: common.HexToAddress(d.VerifyingContract).Hex(),
		},
		Message: apitypes.TypedDataMessage{
			"from":        a.From,
			"to":          a.To,
			"value":       a.Value,
			"validAfter":  a.ValidAfter,
			"validBefore": a.ValidBefore,
			"nonce":       a.Nonce,
		},
	}
}

// AuthorizationDigest is the EIP-712 hash the signature covers.
func AuthorizationDigest(d Domain, a x402.Authorization) ([]byte, error) {
	hash, _, err := apitypes.TypedDataAndHash(typedData(d, a))
	return hash, err
}

// SignAuthorization signs a with pk over d and returns a 0x 65-byte
// signature with v in {27,28}, the form EIP-3009 tokens and the facilitator
// expect.
func SignAuthorization(pk *ecdsa.PrivateKey, d Domain, a x402.Authorization) (string, error) {
	digest, err := AuthorizationDigest(d, a)
	if err != nil {
		return "", fmt.Errorf("eip-712 hash: %w", err)
	}
	sig, err := crypto.Sign(digest, pk)
	if err != nil {
		return "", err
	}
	sig[64] += 27
	return hexutil.Encode(sig), nil
}

// RecoverAuthorizationSigner returns the address that produced sig over a.
func RecoverAuthorizationSigner(d Domain, a x402.Authorization, sig string) (string, error) {
	raw, err := hexutil.Decode(sig)
	if err != nil || len(raw) != 65 {
		return "", errors.New("signature must be 65 bytes of 0x hex")
	}
	digest, err := AuthorizationDigest(d, a)
	if err != nil {
		return "", err
	}
	raw = bytes.Clone(raw)
	if raw[64] >= 27 {
		raw[64] -= 27
	}
	pub, err := crypto.SigToPub(digest, raw)
	if err != nil {
		return "", err
	}
	return crypto.PubkeyToAddress(*pub).Hex(), nil
}

// --- read-only chain access -------------------------------------------------

func (s *Service) rpc(ctx context.Context, method string, params []any, out any) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("%s: decode: %w", method, err)
	}
	if env.Error != nil {
		return fmt.Errorf("%s: %s", method, env.Error.Message)
	}
	return json.Unmarshal(env.Result, out)
}

// TokenBalance returns address's USDT balance in base units (6 decimals).
func (s *Service) TokenBalance(ctx context.Context, address string) (uint64, error) {
	if !chain.IsAddress(address) {
		return 0, fmt.Errorf("invalid address %q", address)
	}
	// balanceOf(address) selector 0x70a08231
	data := "0x70a08231" + strings.Repeat("0", 24) + strings.ToLower(strings.TrimPrefix(common.HexToAddress(address).Hex(), "0x"))
	var res string
	if err := s.rpc(ctx, "eth_call", []any{map[string]string{"to": chain.USDTAddress, "data": data}, "latest"}, &res); err != nil {
		return 0, err
	}
	n, err := hexutil.DecodeBig(normalizeQuantity(res))
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// NativeBalance returns address's WBT balance in wei.
func (s *Service) NativeBalance(ctx context.Context, address string) (*big.Int, error) {
	if !chain.IsAddress(address) {
		return nil, fmt.Errorf("invalid address %q", address)
	}
	var res string
	if err := s.rpc(ctx, "eth_getBalance", []any{address, "latest"}, &res); err != nil {
		return nil, err
	}
	return hexutil.DecodeBig(normalizeQuantity(res))
}

// Balance is the USDT balance; kept as the generic name the handlers use.
func (s *Service) Balance(ctx context.Context, address string) (uint64, error) {
	return s.TokenBalance(ctx, address)
}

// normalizeQuantity strips leading zeros from a 0x hex word so hexutil's
// strict quantity decoder accepts a 32-byte eth_call result.
func normalizeQuantity(h string) string {
	t := strings.TrimLeft(strings.TrimPrefix(h, "0x"), "0")
	if t == "" {
		return "0x0"
	}
	return "0x" + t
}
