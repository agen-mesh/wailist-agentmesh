package arc

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Caller is a JSON-RPC client. Tests substitute a fake chain.
type Caller interface {
	Call(ctx context.Context, url, method string, params []any, out any) error
}

// HTTPCaller talks to the chain RPCs in the registry. It does not accept
// a caller-supplied URL.
type HTTPCaller struct {
	HTTP *http.Client
}

func (h HTTPCaller) Call(ctx context.Context, url, method string, params []any, out any) error {
	if url == "" {
		return errors.New("arc: chain has no RPC")
	}
	client := h.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("arc: rpc %s: http %d", method, resp.StatusCode)
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("arc: rpc %s: %w", method, err)
	}
	if envelope.Error != nil {
		if envelope.JSONRPC != "2.0" || string(envelope.ID) != "1" || len(envelope.Result) != 0 {
			return fmt.Errorf("arc: rpc %s: malformed error response", method)
		}
		return fmt.Errorf("arc: rpc %s: %w", method, envelope.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return fmt.Errorf("arc: rpc %s result: %w", method, err)
	}
	return nil
}

func parseHex(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if s == "" {
		return big.NewInt(0), nil
	}
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return nil, fmt.Errorf("arc: bad hex quantity %q", s)
	}
	return n, nil
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return hex.DecodeString(s)
}

func encodeHex(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

var (
	erc20ABI   = mustABI(`[{"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"type":"uint256"}]},{"type":"function","name":"allowance","stateMutability":"view","inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"}],"outputs":[{"type":"uint256"}]},{"type":"function","name":"approve","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"type":"bool"}]},{"type":"function","name":"transfer","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"type":"bool"}]},{"type":"function","name":"transferWithAuthorization","inputs":[{"name":"from","type":"address"},{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"validAfter","type":"uint256"},{"name":"validBefore","type":"uint256"},{"name":"nonce","type":"bytes32"},{"name":"v","type":"uint8"},{"name":"r","type":"bytes32"},{"name":"s","type":"bytes32"}]}]`)
	cctpABI    = mustABI(`[{"type":"function","name":"depositForBurn","inputs":[{"name":"amount","type":"uint256"},{"name":"destinationDomain","type":"uint32"},{"name":"mintRecipient","type":"bytes32"},{"name":"burnToken","type":"address"},{"name":"destinationCaller","type":"bytes32"},{"name":"maxFee","type":"uint256"},{"name":"minFinalityThreshold","type":"uint32"}]},{"type":"function","name":"receiveMessage","inputs":[{"name":"message","type":"bytes"},{"name":"attestation","type":"bytes"}],"outputs":[{"type":"bool"}]}]`)
	gatewayABI = mustABI(`[{"type":"function","name":"deposit","inputs":[{"name":"token","type":"address"},{"name":"value","type":"uint256"}]}]`)
	stringABI  = mustType("string")
	uintABI    = mustType("uint256")
)

func mustABI(jsonABI string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(jsonABI))
	if err != nil {
		panic(err)
	}
	return parsed
}

func mustType(t string) abi.Type {
	parsed, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return parsed
}

func unpackUint(data []byte) (*big.Int, error) {
	vals, err := abi.Arguments{{Type: uintABI}}.Unpack(data)
	if err != nil {
		return nil, err
	}
	n, ok := vals[0].(*big.Int)
	if !ok || n == nil {
		return nil, errors.New("arc: uint decode")
	}
	return n, nil
}

func unpackString(data []byte) (string, error) {
	vals, err := abi.Arguments{{Type: stringABI}}.Unpack(data)
	if err != nil {
		return "", err
	}
	s, ok := vals[0].(string)
	if !ok {
		return "", errors.New("arc: string decode")
	}
	return s, nil
}

// ERC20Balance reads balanceOf. The token's own decimals are 6 for the
// assets this package transfers.
func ERC20Balance(ctx context.Context, caller Caller, chain Chain, token, holder common.Address) (*big.Int, error) {
	data, err := erc20ABI.Pack("balanceOf", holder)
	if err != nil {
		return nil, err
	}
	raw, err := ethCall(ctx, caller, chain, token, data)
	if err != nil {
		return nil, err
	}
	return unpackUint(raw)
}

func ethCall(ctx context.Context, caller Caller, chain Chain, to common.Address, data []byte) ([]byte, error) {
	var hexResult string
	err := caller.Call(ctx, chain.RPC, "eth_call", []any{
		map[string]string{"to": to.Hex(), "data": encodeHex(data)},
		"latest",
	}, &hexResult)
	if err != nil {
		return nil, err
	}
	return decodeHex(hexResult)
}

func tokenString(ctx context.Context, caller Caller, chain Chain, token common.Address, method string) (string, error) {
	// name() and version() selectors. Packed via the ABI when present;
	// these two are not in erc20ABI, so the selectors are the canonical ones.
	var sel []byte
	switch method {
	case "name":
		sel = crypto.Keccak256([]byte("name()"))[:4]
	case "version":
		sel = crypto.Keccak256([]byte("version()"))[:4]
	default:
		return "", fmt.Errorf("arc: unknown token string %s", method)
	}
	raw, err := ethCall(ctx, caller, chain, token, sel)
	if err != nil {
		return "", err
	}
	return unpackString(raw)
}

// Authorization is an EIP-3009 transferWithAuthorization.
type Authorization struct {
	From, To                       common.Address
	Value, ValidAfter, ValidBefore *big.Int
	Nonce                          common.Hash
	V                              uint8
	R, S                           common.Hash
}

func (a Authorization) signatureHex() string {
	var buf [65]byte
	copy(buf[0:32], a.R[:])
	copy(buf[32:64], a.S[:])
	buf[64] = a.V
	return encodeHex(buf[:])
}

// TypedDataHash is the EIP-712 digest the signer signs.
func TypedDataHash(name, version string, chainID int64, verifying common.Address, a Authorization) []byte {
	domainType := crypto.Keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	domain := crypto.Keccak256(concat(
		domainType,
		crypto.Keccak256([]byte(name)),
		crypto.Keccak256([]byte(version)),
		u256(big.NewInt(chainID)),
		addrWord(verifying),
	))
	typeHash := crypto.Keccak256([]byte("TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)"))
	structHash := crypto.Keccak256(concat(
		typeHash,
		addrWord(a.From),
		addrWord(a.To),
		u256(a.Value),
		u256(a.ValidAfter),
		u256(a.ValidBefore),
		a.Nonce.Bytes(),
	))
	return crypto.Keccak256(concat([]byte{0x19, 0x01}, domain, structHash))
}

func concat(parts ...[]byte) []byte {
	var n int
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u256(n *big.Int) []byte {
	buf := make([]byte, 32)
	if n != nil && n.Sign() > 0 {
		n.FillBytes(buf)
	}
	return buf
}

func addrWord(a common.Address) []byte {
	buf := make([]byte, 32)
	copy(buf[12:], a.Bytes())
	return buf
}

// SignAuthorization signs EIP-3009. v in the returned struct is 27 or 28,
// which is what the USDC contract expects.
func SignAuthorization(key *ecdsa.PrivateKey, name, version string, chainID int64, verifying common.Address, a Authorization) (Authorization, error) {
	if key == nil {
		return Authorization{}, errors.New("arc: no signing key")
	}
	a.From = crypto.PubkeyToAddress(key.PublicKey)
	digest := TypedDataHash(name, version, chainID, verifying, a)
	sig, err := crypto.Sign(digest, key)
	if err != nil {
		return Authorization{}, err
	}
	copy(a.R[:], sig[:32])
	copy(a.S[:], sig[32:64])
	a.V = sig[64] + 27
	return a, nil
}

// RecoverAuthorizationSigner returns the address that signed the authorization.
func RecoverAuthorizationSigner(name, version string, chainID int64, verifying common.Address, a Authorization) (common.Address, error) {
	digest := TypedDataHash(name, version, chainID, verifying, a)
	sig := make([]byte, 65)
	copy(sig[:32], a.R[:])
	copy(sig[32:64], a.S[:])
	if a.V >= 27 {
		sig[64] = a.V - 27
	} else {
		sig[64] = a.V
	}
	pub, err := crypto.SigToPub(digest, sig)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pub), nil
}

func packTransfer(to common.Address, amount *big.Int) ([]byte, error) {
	return erc20ABI.Pack("transfer", to, amount)
}

func packApprove(spender common.Address, amount *big.Int) ([]byte, error) {
	return erc20ABI.Pack("approve", spender, amount)
}

func packTransferWithAuthorization(a Authorization) ([]byte, error) {
	return erc20ABI.Pack("transferWithAuthorization",
		a.From, a.To, a.Value, a.ValidAfter, a.ValidBefore, a.Nonce, a.V, a.R, a.S)
}

func packDepositForBurn(amount *big.Int, destDomain uint32, recipient, burnToken common.Address) ([]byte, error) {
	var mint, caller common.Hash
	copy(mint[12:], recipient.Bytes())
	return cctpABI.Pack("depositForBurn", amount, destDomain, mint, burnToken, caller, big.NewInt(0), FinalityStandard)
}

func packReceive(message, attestation []byte) ([]byte, error) {
	return cctpABI.Pack("receiveMessage", message, attestation)
}

func packGatewayDeposit(token common.Address, amount *big.Int) ([]byte, error) {
	return gatewayABI.Pack("deposit", token, amount)
}

func allowance(ctx context.Context, caller Caller, chain Chain, token, owner, spender common.Address) (*big.Int, error) {
	data, err := erc20ABI.Pack("allowance", owner, spender)
	if err != nil {
		return nil, err
	}
	raw, err := ethCall(ctx, caller, chain, token, data)
	if err != nil {
		return nil, err
	}
	return unpackUint(raw)
}

// SendContract submits a contract call from key. valueWei is the native
// value, which is zero for every USDC movement in this package.
func SendContract(ctx context.Context, caller Caller, chain Chain, key *ecdsa.PrivateKey, to common.Address, data []byte) (hash string, err error) {
	sendAttempted := false
	defer func() {
		if err != nil && !sendAttempted {
			err = fmt.Errorf("%w: %w", ErrNotSubmitted, err)
		}
	}()
	if key == nil {
		return "", errors.New("arc: spending key is not configured")
	}
	from := crypto.PubkeyToAddress(key.PublicKey)
	var nonceHex, gasHex, tipHex string
	if err := caller.Call(ctx, chain.RPC, "eth_getTransactionCount", []any{from.Hex(), "pending"}, &nonceHex); err != nil {
		return "", err
	}
	nonce, err := parseHex(nonceHex)
	if err != nil {
		return "", err
	}
	callObj := map[string]string{"from": from.Hex(), "to": to.Hex(), "data": encodeHex(data), "value": "0x0"}
	if err := caller.Call(ctx, chain.RPC, "eth_estimateGas", []any{callObj}, &gasHex); err != nil {
		return "", err
	}
	gas, err := parseHex(gasHex)
	if err != nil {
		return "", err
	}
	gas.Mul(gas, big.NewInt(12))
	gas.Div(gas, big.NewInt(10))
	if gas.Cmp(big.NewInt(100_000)) < 0 {
		gas = big.NewInt(100_000)
	}
	var priority *big.Int
	if err := caller.Call(ctx, chain.RPC, "eth_maxPriorityFeePerGas", []any{}, &tipHex); err == nil {
		if parsed, perr := parseHex(tipHex); perr == nil && parsed.Sign() > 0 {
			priority = parsed
		}
	}
	var priceHex string
	var gasPrice *big.Int
	if err := caller.Call(ctx, chain.RPC, "eth_gasPrice", []any{}, &priceHex); err == nil {
		if parsed, perr := parseHex(priceHex); perr == nil && parsed.Sign() > 0 {
			gasPrice = parsed
		}
	}
	tip, feeCap := FeeSuggestion(chain.ChainID, priority, gasPrice)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(chain.ChainID),
		Nonce:     nonce.Uint64(),
		GasTipCap: tip,
		GasFeeCap: feeCap,
		Gas:       gas.Uint64(),
		To:        &to,
		Value:     big.NewInt(0),
		Data:      data,
	})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(chain.ChainID)), key)
	if err != nil {
		return "", err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return "", err
	}
	sendAttempted = true
	if err := caller.Call(ctx, chain.RPC, "eth_sendRawTransaction", []any{encodeHex(raw)}, &hash); err != nil {
		if definitiveRejection(err) {
			return "", fmt.Errorf("%w: %w", ErrNotSubmitted, err)
		}
		return signed.Hash().Hex(), fmt.Errorf("%w: %w", ErrSubmissionUncertain, err)
	}
	if !strings.EqualFold(hash, signed.Hash().Hex()) {
		return signed.Hash().Hex(), fmt.Errorf("%w: RPC returned a different transaction identity", ErrSubmissionUncertain)
	}
	return hash, nil
}

// FeeSuggestion builds the EIP-1559 tip and fee cap. priorityFee and gasPrice
// may be nil when the RPC omitted them. On Arc chain ids 5042 and 5042002
// both values are at least 20 gwei, which is the chain's minimum base fee.
func FeeSuggestion(chainID int64, priorityFee, gasPrice *big.Int) (tip, feeCap *big.Int) {
	tip = big.NewInt(1_000_000_000)
	if priorityFee != nil && priorityFee.Sign() > 0 {
		tip = new(big.Int).Set(priorityFee)
	}
	feeCap = new(big.Int).Set(tip)
	if gasPrice != nil && gasPrice.Sign() > 0 {
		feeCap = new(big.Int).Mul(gasPrice, big.NewInt(2))
	}
	if chainID == 5042 || chainID == 5042002 {
		floor := big.NewInt(20_000_000_000)
		if tip.Cmp(floor) < 0 {
			tip = new(big.Int).Set(floor)
		}
		if feeCap.Cmp(floor) < 0 {
			feeCap = new(big.Int).Set(floor)
		}
	}
	if feeCap.Cmp(tip) < 0 {
		feeCap = new(big.Int).Set(tip)
	}
	return tip, feeCap
}

// ReceiptStatus polls until the transaction is final or the wait expires.
// "pending" means inclusion has not been established.
func ReceiptStatus(ctx context.Context, caller Caller, chain Chain, hash string) (string, error) {
	wait := 45 * time.Second
	every := 2 * time.Second
	if chain.ChainID == 5042 || chain.ChainID == 5042002 {
		wait = 20 * time.Second
		every = 400 * time.Millisecond
	}
	deadline := time.Now().Add(wait)
	for {
		var receipt *struct {
			Status string `json:"status"`
		}
		err := caller.Call(ctx, chain.RPC, "eth_getTransactionReceipt", []any{hash}, &receipt)
		if err == nil && receipt != nil && receipt.Status != "" {
			if receipt.Status == "0x1" || receipt.Status == "0x01" {
				return "success", nil
			}
			if receipt.Status == "0x0" || receipt.Status == "0x00" {
				return "reverted", fmt.Errorf("%w: transaction %s", ErrTransactionReverted, hash)
			}
			return "pending", fmt.Errorf("%w: invalid receipt status", ErrSubmissionUncertain)
		}
		if time.Now().After(deadline) {
			return "pending", nil
		}
		select {
		case <-ctx.Done():
			return "pending", ctx.Err()
		case <-time.After(every):
		}
	}
}
