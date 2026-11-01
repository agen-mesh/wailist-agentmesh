package arc

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Config is the process-wide Arc setup. The private key is never serialised.
type Config struct {
	Network         Network
	MaxMicros       *big.Int
	StudioURL       string
	Escrow          string
	PayTo           string
	GatewayURL      string
	IrisURL         string
	Key             *ecdsa.PrivateKey
	Address         common.Address
	KeyError        string
	Warnings        []string
	operatorUserIDs map[string]struct{}
}

// LoadConfig reads ARC_* from the environment. An unusable key is reported
// on KeyError and does not stop balance reads.
func LoadConfig() Config {
	cfg := Config{
		Network:         ParseNetwork(os.Getenv("ARC_NETWORK")),
		StudioURL:       strings.TrimSpace(os.Getenv("ARC_STUDIO_URL")),
		operatorUserIDs: make(map[string]struct{}),
	}
	for _, id := range strings.Split(os.Getenv("ARC_OPERATOR_USER_IDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			cfg.operatorUserIDs[id] = struct{}{}
		}
	}
	maxRaw := strings.TrimSpace(os.Getenv("ARC_MAX_USDC"))
	if maxRaw == "" {
		maxRaw = "1000"
	}
	max, err := ParseUSDCMicros(maxRaw)
	if err != nil || max.Sign() <= 0 {
		max, _ = ParseUSDCMicros("1000")
		cfg.Warnings = append(cfg.Warnings, "ARC_MAX_USDC is invalid; using 1000")
	}
	cfg.MaxMicros = max
	if raw := strings.TrimSpace(os.Getenv("ARC_SPEND_PRIVATE_KEY")); raw != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(raw, "0x"))
		if err != nil {
			cfg.KeyError = "ARC_SPEND_PRIVATE_KEY is not a 32-byte hex private key"
		} else {
			cfg.Key = key
			cfg.Address = crypto.PubkeyToAddress(key.PublicKey)
		}
	}
	if cfg.Key != nil && len(cfg.operatorUserIDs) == 0 {
		cfg.Warnings = append(cfg.Warnings, "ARC_OPERATOR_USER_IDS is empty while the Arc spending key is set; workflow owners can broadcast their own Arc nodes, and direct transfers, bridges, and nanopays stay denied")
	}
	if pay := strings.TrimSpace(os.Getenv("ARC_PAYTO")); common.IsHexAddress(pay) {
		cfg.PayTo = common.HexToAddress(pay).Hex()
	} else if cfg.Address != (common.Address{}) {
		cfg.PayTo = cfg.Address.Hex()
	}
	if esc := strings.TrimSpace(os.Getenv("ARC_ESCROW_ADDRESS")); common.IsHexAddress(esc) {
		cfg.Escrow = common.HexToAddress(esc).Hex()
	}
	return cfg
}

func (c Config) WalletConfigured() bool {
	return c.Key != nil && c.KeyError == ""
}

// Request is one treasury operation. Broadcast stays false unless the caller
// sets it, so a prepared workflow node does not send a transaction.
type Request struct {
	Op          string
	ChainID     string
	DestChainID string
	To          string
	Amount      string
	Broadcast   bool
}

// Result is the JSON shape returned to the canvas and the treasury page.
type Result struct {
	Status      string         `json:"status"`
	Op          string         `json:"op"`
	Chain       string         `json:"chain"`
	DestChain   string         `json:"destChain,omitempty"`
	Amount      string         `json:"amount"`
	Micros      string         `json:"micros"`
	To          string         `json:"to,omitempty"`
	From        string         `json:"from,omitempty"`
	Asset       string         `json:"asset,omitempty"`
	CAIP2       string         `json:"caip2,omitempty"`
	Calldata    string         `json:"calldata,omitempty"`
	ApproveData string         `json:"approveCalldata,omitempty"`
	TxHash      string         `json:"txHash,omitempty"`
	ApproveTx   string         `json:"approveTx,omitempty"`
	Explorer    string         `json:"explorer,omitempty"`
	Signature   string         `json:"signature,omitempty"`
	TypedData   map[string]any `json:"typedData,omitempty"`
	Domain      string         `json:"domainSource,omitempty"`
	Note        string         `json:"note,omitempty"`
}

type prepared struct {
	op        string
	chain     Chain
	dest      *Chain
	amount    *big.Int
	to        common.Address
	call      []byte
	callTo    common.Address
	approve   []byte
	approveTo common.Address
}

// Prepare validates a request and builds the calldata. It does not sign and
// does not talk to a network.
func Prepare(cfg Config, req Request) (prepared, error) {
	var out prepared
	op := strings.TrimSpace(req.Op)
	if op == "" {
		op = "nanopay"
	}
	out.op = op
	chainID := strings.TrimSpace(req.ChainID)
	if chainID == "" {
		if cfg.Network == Mainnet {
			chainID = "arc"
		} else {
			chainID = "arc-testnet"
		}
	}
	chain, err := resolve(cfg, chainID)
	if err != nil {
		return prepared{}, err
	}
	out.chain = chain
	if op == "bridge" && !chain.CCTP() {
		return prepared{}, fmt.Errorf("%s is not a CCTP domain; USDC there cannot be burned for minting on another chain", chain.Name)
	}
	if chain.Family != "evm" {
		return prepared{}, fmt.Errorf("%s does not settle EVM USDC; it stays in the treasury as an Algorand balance", chain.Name)
	}
	amount, err := ParseUSDCMicros(req.Amount)
	if err != nil {
		return prepared{}, err
	}
	if amount.Sign() <= 0 {
		return prepared{}, errors.New("amount must be greater than zero")
	}
	if cfg.MaxMicros != nil && amount.Cmp(cfg.MaxMicros) > 0 {
		return prepared{}, fmt.Errorf("amount %s USDC exceeds the cap of %s", FormatUSDC(amount), FormatUSDC(cfg.MaxMicros))
	}
	out.amount = amount
	switch op {
	case "nanopay", "transfer":
		to, err := parseAddr(req.To)
		if err != nil {
			return prepared{}, err
		}
		out.to = to
		data, err := packTransfer(to, amount)
		if err != nil {
			return prepared{}, err
		}
		out.call = data
		out.callTo = common.HexToAddress(chain.USDC)
	case "bridge":
		if !chain.CCTP() {
			return prepared{}, fmt.Errorf("%s is not a CCTP domain; USDC there cannot be burned for minting on another chain", chain.Name)
		}
		dest, err := resolve(cfg, req.DestChainID)
		if err != nil {
			return prepared{}, err
		}
		if !dest.CCTP() {
			return prepared{}, fmt.Errorf("%s is not a CCTP domain; USDC cannot be minted there", dest.Name)
		}
		if dest.ID == chain.ID {
			return prepared{}, errors.New("source and destination are the same chain")
		}
		to, err := parseAddr(req.To)
		if err != nil {
			return prepared{}, err
		}
		out.dest = &dest
		out.to = to
		approve, err := packApprove(common.HexToAddress(chain.TokenMessenger), amount)
		if err != nil {
			return prepared{}, err
		}
		out.approve = approve
		out.approveTo = common.HexToAddress(chain.USDC)
		data, err := packDepositForBurn(amount, uint32(dest.Domain), to, common.HexToAddress(chain.USDC))
		if err != nil {
			return prepared{}, err
		}
		out.call = data
		out.callTo = common.HexToAddress(chain.TokenMessenger)
	case "gateway":
		if !chain.Gateway() {
			return prepared{}, fmt.Errorf("%s has no Gateway wallet", chain.Name)
		}
		approve, err := packApprove(common.HexToAddress(chain.GatewayWallet), amount)
		if err != nil {
			return prepared{}, err
		}
		out.approve = approve
		out.approveTo = common.HexToAddress(chain.USDC)
		data, err := packGatewayDeposit(common.HexToAddress(chain.USDC), amount)
		if err != nil {
			return prepared{}, err
		}
		out.call = data
		out.callTo = common.HexToAddress(chain.GatewayWallet)
		if cfg.WalletConfigured() {
			out.to = cfg.Address
		}
	default:
		return prepared{}, fmt.Errorf("unknown arc operation %q", op)
	}
	return out, nil
}

func resolve(cfg Config, id string) (Chain, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "arc-testnet"
		if cfg.Network == Mainnet {
			id = "arc"
		}
	}
	if c, ok := ChainByID(cfg.Network, id); ok {
		return c, nil
	}
	other := Mainnet
	if cfg.Network == Mainnet {
		other = Testnet
	}
	if _, ok := ChainByID(other, id); ok {
		mode := cfg.Network
		if mode == "" {
			mode = Testnet
		}
		return Chain{}, fmt.Errorf("chain %s is not available while ARC_NETWORK=%s", id, mode)
	}
	return Chain{}, fmt.Errorf("unknown chain %q", id)
}

func parseAddr(s string) (common.Address, error) {
	if !common.IsHexAddress(strings.TrimSpace(s)) {
		return common.Address{}, errors.New("recipient must be a 20-byte hex address")
	}
	a := common.HexToAddress(s)
	if a == (common.Address{}) {
		return common.Address{}, errors.New("recipient is the zero address")
	}
	if a == common.HexToAddress(Blocklisted) {
		return common.Address{}, errors.New("recipient is blocklisted on Arc and the transfer would revert")
	}
	return a, nil
}

func (p prepared) quote(domain string) Result {
	res := Result{
		Status:   "quoted",
		Op:       p.op,
		Chain:    p.chain.ID,
		Amount:   FormatUSDC(p.amount),
		Micros:   p.amount.String(),
		Asset:    p.chain.USDC,
		CAIP2:    p.chain.CAIP2(),
		Calldata: encodeHex(p.call),
		Domain:   domain,
		Note:     "Quoted only. Set broadcast to send it.",
	}
	if p.to != (common.Address{}) {
		res.To = p.to.Hex()
	}
	if p.dest != nil {
		res.DestChain = p.dest.ID
	}
	if len(p.approve) > 0 {
		res.ApproveData = encodeHex(p.approve)
	}
	if p.op == "nanopay" {
		// The transfer calldata is not the nanopayment. The authorization is
		// signed only when a broadcast reads the token's real domain name.
		res.Calldata = ""
		res.TypedData = map[string]any{
			"types": map[string]any{
				"EIP712Domain": []map[string]string{
					{"name": "name", "type": "string"},
					{"name": "version", "type": "string"},
					{"name": "chainId", "type": "uint256"},
					{"name": "verifyingContract", "type": "address"},
				},
				"TransferWithAuthorization": []map[string]string{
					{"name": "from", "type": "address"},
					{"name": "to", "type": "address"},
					{"name": "value", "type": "uint256"},
					{"name": "validAfter", "type": "uint256"},
					{"name": "validBefore", "type": "uint256"},
					{"name": "nonce", "type": "bytes32"},
				},
			},
			"primaryType": "TransferWithAuthorization",
			"domain": map[string]any{
				"name": "USDC", "version": "2",
				"chainId": p.chain.ChainID, "verifyingContract": p.chain.USDC,
			},
			"message": map[string]any{
				"to": p.to.Hex(), "value": p.amount.String(),
			},
		}
		res.Domain = "default"
		res.Note = "Unsigned EIP-3009 quote. The domain name is refreshed from the token before a signature is made. Broadcast is off."
	}
	return res
}

// Execute prepares a request and, when Broadcast is set, signs and submits it
// with the platform key. The key pays gas. On Arc that gas is USDC.
func Execute(ctx context.Context, caller Caller, cfg Config, req Request) (Result, error) {
	result, err := execute(ctx, caller, cfg, req)
	if err != nil && !errors.Is(err, ErrSubmissionUncertain) && !errors.Is(err, ErrTransactionReverted) && !errors.Is(err, ErrNotSubmitted) {
		err = fmt.Errorf("%w: %w", ErrNotSubmitted, err)
	}
	return result, err
}

func execute(ctx context.Context, caller Caller, cfg Config, req Request) (Result, error) {
	if req.Broadcast {
		if err := cfg.requireOperator(ctx); err != nil {
			return Result{}, err
		}
	}
	p, err := Prepare(cfg, req)
	if err != nil {
		return Result{}, err
	}
	if !req.Broadcast {
		return p.quote("default"), nil
	}
	if !cfg.WalletConfigured() {
		if cfg.KeyError != "" {
			return Result{}, errors.New(cfg.KeyError)
		}
		return Result{}, errors.New("arc spending key is not configured")
	}
	var result Result
	switch p.op {
	case "transfer":
		result, err = submitOne(ctx, caller, cfg, p, p.callTo, p.call)
	case "nanopay":
		result, err = submitNanopay(ctx, caller, cfg, p)
	case "bridge", "gateway":
		result, err = submitApproved(ctx, caller, cfg, p)
	default:
		return Result{}, fmt.Errorf("unknown arc operation %q", p.op)
	}
	if err != nil {
		if submissionUncertain(result, err) && !errors.Is(err, ErrSubmissionUncertain) {
			err = fmt.Errorf("%w: %w", ErrSubmissionUncertain, err)
		}
		// A confirmed revert already has a receipt. Keep that hash. Only a
		// submission the node never accepted loses its hash.
		if submissionUncertain(result, err) || result.Status == "reverted" {
			return result, err
		}
		result.TxHash = ""
		result.Explorer = ""
		if result.Status == "pending" || result.Status == "quoted" {
			result.Status = "rejected"
		}
		return result, err
	}
	return result, nil
}

func submissionUncertain(result Result, err error) bool {
	if errors.Is(err, ErrSubmissionUncertain) {
		return true
	}
	return result.Status == "pending" && (result.TxHash != "" || result.ApproveTx != "")
}

func submitOne(ctx context.Context, caller Caller, cfg Config, p prepared, to common.Address, data []byte) (Result, error) {
	hash, err := SendContract(ctx, caller, p.chain, cfg.Key, to, data)
	res := p.quote("on-chain")
	res.From = cfg.Address.Hex()
	res.Note = ""
	if hash == "" {
		if err == nil {
			err = errors.New("arc: transaction was not accepted")
		}
		return res, err
	}
	res.TxHash = hash
	res.Explorer = p.chain.TxURL(hash)
	if err != nil {
		res.Status = "pending"
		if !errors.Is(err, ErrSubmissionUncertain) {
			err = fmt.Errorf("%w: %w", ErrSubmissionUncertain, err)
		}
		return res, err
	}
	status, receiptErr := ReceiptStatus(ctx, caller, p.chain, hash)
	res.Status = status
	if status == "reverted" {
		return res, receiptErr
	}
	if receiptErr != nil || status != "success" {
		res.Status = "pending"
		if receiptErr == nil {
			receiptErr = errors.New("arc: inclusion unknown")
		}
		return res, fmt.Errorf("%w: %w", ErrSubmissionUncertain, receiptErr)
	}
	return res, nil
}

func submitApproved(ctx context.Context, caller Caller, cfg Config, p prepared) (Result, error) {
	owner := cfg.Address
	spender := p.callTo
	token := common.HexToAddress(p.chain.USDC)
	current, err := allowance(ctx, caller, p.chain, token, owner, spender)
	if err != nil {
		return Result{}, err
	}
	res := p.quote("on-chain")
	res.From = owner.Hex()
	res.Note = ""
	approveHash := ""
	if current.Cmp(p.amount) < 0 {
		approveHash, err = SendContract(ctx, caller, p.chain, cfg.Key, p.approveTo, p.approve)
		if err != nil {
			if approveHash == "" {
				return res, err
			}
			res.Status = "pending"
			res.ApproveTx = approveHash
			if !errors.Is(err, ErrSubmissionUncertain) {
				err = fmt.Errorf("%w: %w", ErrSubmissionUncertain, err)
			}
			return res, err
		}
		status, receiptErr := ReceiptStatus(ctx, caller, p.chain, approveHash)
		res.ApproveTx = approveHash
		if status == "reverted" {
			res.Status = status
			return res, receiptErr
		}
		if receiptErr != nil || status == "pending" {
			res.Status = "pending"
			if receiptErr == nil {
				receiptErr = errors.New("arc: inclusion unknown")
			}
			return res, fmt.Errorf("%w: %w", ErrSubmissionUncertain, receiptErr)
		}
	}
	sent, err := submitOne(ctx, caller, cfg, p, p.callTo, p.call)
	sent.ApproveTx = approveHash
	return sent, err
}

func submitNanopay(ctx context.Context, caller Caller, cfg Config, p prepared) (Result, error) {
	token := common.HexToAddress(p.chain.USDC)
	name, err := tokenString(ctx, caller, p.chain, token, "name")
	if err != nil {
		return Result{}, fmt.Errorf("read USDC name(): %w", err)
	}
	version, err := tokenString(ctx, caller, p.chain, token, "version")
	if err != nil {
		return Result{}, fmt.Errorf("read USDC version(): %w", err)
	}
	var nonce common.Hash
	if _, err := rand.Read(nonce[:]); err != nil {
		return Result{}, err
	}
	auth := Authorization{
		To:          p.to,
		Value:       p.amount,
		ValidAfter:  big.NewInt(0),
		ValidBefore: big.NewInt(time.Now().Add(time.Hour).Unix()),
		Nonce:       nonce,
	}
	signed, err := SignAuthorization(cfg.Key, name, version, p.chain.ChainID, token, auth)
	if err != nil {
		return Result{}, err
	}
	if !rememberNonce(signed.Nonce) {
		return Result{}, errors.New("authorization nonce was already used")
	}
	data, err := packTransferWithAuthorization(signed)
	if err != nil {
		return Result{}, err
	}
	res, err := submitOne(ctx, caller, cfg, p, token, data)
	res.From = signed.From.Hex()
	res.Signature = signed.signatureHex()
	res.Domain = "token"
	if res.TypedData != nil {
		res.TypedData["domain"] = map[string]any{
			"name": name, "version": version,
			"chainId": p.chain.ChainID, "verifyingContract": token.Hex(),
		}
		res.TypedData["message"] = map[string]any{
			"from": signed.From.Hex(), "to": signed.To.Hex(),
			"value":       signed.Value.String(),
			"validAfter":  signed.ValidAfter.String(),
			"validBefore": signed.ValidBefore.String(),
			"nonce":       signed.Nonce.Hex(),
		}
	}
	return res, err
}

var usedNonces sync.Map

func rememberNonce(n common.Hash) bool {
	_, loaded := usedNonces.LoadOrStore(strings.ToLower(n.Hex()), struct{}{})
	return !loaded
}

// Challenge is the 402 body for the Arc nanopayment resource.
type Challenge struct {
	X402Version int              `json:"x402Version"`
	Error       string           `json:"error,omitempty"`
	Accepts     []map[string]any `json:"accepts"`
	Resource    string           `json:"resource"`
}

func NewChallenge(cfg Config, amount *big.Int, resource string) (Challenge, error) {
	if cfg.PayTo == "" {
		return Challenge{}, errors.New("arc spending key is not configured")
	}
	chain, err := resolve(cfg, "")
	if err != nil {
		return Challenge{}, err
	}
	if amount == nil || amount.Sign() <= 0 {
		amount = big.NewInt(10_000)
	}
	if cfg.MaxMicros != nil && amount.Cmp(cfg.MaxMicros) > 0 {
		return Challenge{}, fmt.Errorf("amount exceeds the cap of %s USDC", FormatUSDC(cfg.MaxMicros))
	}
	return Challenge{
		X402Version: 2,
		Error:       "Payment required",
		Resource:    resource,
		Accepts: []map[string]any{{
			"scheme":            "exact",
			"network":           chain.CAIP2(),
			"asset":             chain.USDC,
			"maxAmountRequired": amount.String(),
			"payTo":             cfg.PayTo,
			"maxTimeoutSeconds": 3600,
			"extra": map[string]string{
				"name": "USDC", "version": "2",
				"description": "AgentMesh Arc nanopayment. EIP-3009 transferWithAuthorization, gas in USDC.",
			},
		}},
	}, nil
}

// Payment is an x402 exact payload carrying an EIP-3009 signature.
type Payment struct {
	X402Version int    `json:"x402Version"`
	Scheme      string `json:"scheme"`
	Network     string `json:"network"`
	Payload     struct {
		Authorization struct {
			From        string `json:"from"`
			To          string `json:"to"`
			Value       string `json:"value"`
			ValidAfter  string `json:"validAfter"`
			ValidBefore string `json:"validBefore"`
			Nonce       string `json:"nonce"`
		} `json:"authorization"`
		Signature string `json:"signature"`
	} `json:"payload"`
}

// SettlePayment checks an EIP-3009 payment and submits it from the relayer.
// The signer pays the USDC. The relayer pays the gas. The amount cap still
// applies. This is the public x402 settle path, so it does not require an
// operator. Direct spends of the platform key keep the allowlist.
func SettlePayment(ctx context.Context, caller Caller, cfg Config, raw string, price *big.Int) (Result, error) {
	if !cfg.WalletConfigured() {
		return Result{}, errors.New("arc spending key is not configured")
	}
	var payment Payment
	if err := json.Unmarshal([]byte(raw), &payment); err != nil {
		return Result{}, errors.New("X-Payment is not the Arc exact payment JSON")
	}
	if payment.Scheme != "" && payment.Scheme != "exact" {
		return Result{}, errors.New("only the exact scheme is accepted")
	}
	chain, err := resolve(cfg, "")
	if err != nil {
		return Result{}, err
	}
	if payment.Network != "" && payment.Network != chain.CAIP2() {
		return Result{}, fmt.Errorf("payment network %s does not match %s", payment.Network, chain.CAIP2())
	}
	auth, err := payment.authorization()
	if err != nil {
		return Result{}, err
	}
	if auth.To.Hex() != common.HexToAddress(cfg.PayTo).Hex() {
		return Result{}, errors.New("payment recipient does not match the challenge payTo")
	}
	if price != nil && auth.Value.Cmp(price) < 0 {
		return Result{}, errors.New("payment amount is below the price")
	}
	if cfg.MaxMicros != nil && auth.Value.Cmp(cfg.MaxMicros) > 0 {
		return Result{}, fmt.Errorf("amount exceeds the cap of %s USDC", FormatUSDC(cfg.MaxMicros))
	}
	now := time.Now().Unix()
	if auth.ValidBefore.Cmp(big.NewInt(now)) <= 0 || auth.ValidAfter.Cmp(big.NewInt(now)) > 0 {
		return Result{}, errors.New("payment authorization is outside its validity window")
	}
	token := common.HexToAddress(chain.USDC)
	name, err := tokenString(ctx, caller, chain, token, "name")
	if err != nil {
		return Result{}, err
	}
	version, err := tokenString(ctx, caller, chain, token, "version")
	if err != nil {
		return Result{}, err
	}
	signer, err := RecoverAuthorizationSigner(name, version, chain.ChainID, token, auth)
	if err != nil {
		return Result{}, err
	}
	if signer != auth.From {
		return Result{}, errors.New("authorization signature does not match from")
	}
	if !rememberNonce(auth.Nonce) {
		return Result{}, errors.New("authorization nonce was already used")
	}
	data, err := packTransferWithAuthorization(auth)
	if err != nil {
		return Result{}, err
	}
	p := prepared{op: "nanopay", chain: chain, amount: auth.Value, to: auth.To}
	res, err := submitOne(ctx, caller, cfg, p, token, data)
	res.From = auth.From.Hex()
	res.Domain = "token"
	return res, err
}

func (p Payment) authorization() (Authorization, error) {
	from, err := parseAddr(p.Payload.Authorization.From)
	if err != nil {
		return Authorization{}, err
	}
	to, err := parseAddr(p.Payload.Authorization.To)
	if err != nil {
		return Authorization{}, err
	}
	value, ok := new(big.Int).SetString(strings.TrimSpace(p.Payload.Authorization.Value), 10)
	if !ok || value.Sign() <= 0 {
		return Authorization{}, errors.New("payment value must be a positive integer in USDC base units")
	}
	validAfter, ok := new(big.Int).SetString(zeroDefault(p.Payload.Authorization.ValidAfter), 10)
	if !ok {
		return Authorization{}, errors.New("validAfter is not an integer")
	}
	validBefore, ok := new(big.Int).SetString(strings.TrimSpace(p.Payload.Authorization.ValidBefore), 10)
	if !ok {
		return Authorization{}, errors.New("validBefore is not an integer")
	}
	nonceBytes, err := decodeHex(p.Payload.Authorization.Nonce)
	if err != nil || len(nonceBytes) != 32 {
		return Authorization{}, errors.New("nonce must be 32 bytes")
	}
	sig, err := decodeHex(p.Payload.Signature)
	if err != nil || len(sig) != 65 {
		return Authorization{}, errors.New("signature must be 65 bytes")
	}
	var a Authorization
	a.From, a.To = from, to
	a.Value, a.ValidAfter, a.ValidBefore = value, validAfter, validBefore
	copy(a.Nonce[:], nonceBytes)
	copy(a.R[:], sig[:32])
	copy(a.S[:], sig[32:64])
	a.V = sig[64]
	return a, nil
}

func zeroDefault(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "0"
	}
	return s
}

// TokenBalance is one asset on one chain. IncludedInUSDC is true only for a
// successful USDC read. EURC and native gas are shown and not added in.
type TokenBalance struct {
	ChainID        string `json:"chainId"`
	Chain          string `json:"chain"`
	Symbol         string `json:"symbol"`
	Amount         string `json:"amount"`
	Micros         string `json:"micros"`
	Error          string `json:"error,omitempty"`
	IncludedInUSDC bool   `json:"includedInUsdc"`
}

// Treasury is the multichain view. WalletUSDC is the sum of ERC-20 USDC
// balances. GatewayUSDC is Circle Gateway's unified balance, kept separate
// so a deposit is not counted twice.
type Treasury struct {
	Network      string         `json:"network"`
	Address      string         `json:"address,omitempty"`
	Algorand     string         `json:"algorand,omitempty"`
	WalletUSDC   string         `json:"walletUsdc"`
	GatewayUSDC  string         `json:"gatewayUsdc"`
	Partial      bool           `json:"partial"`
	Balances     []TokenBalance `json:"balances"`
	GatewayLines []TokenBalance `json:"gatewayLines,omitempty"`
	GatewayError string         `json:"gatewayError,omitempty"`
}

func ReadTreasury(ctx context.Context, caller Caller, httpClient *http.Client, cfg Config, evmAddr, algoAddr string) (Treasury, error) {
	out := Treasury{Network: string(cfg.Network), WalletUSDC: "0", GatewayUSDC: "0"}
	var holder common.Address
	if strings.TrimSpace(evmAddr) != "" {
		if !common.IsHexAddress(evmAddr) {
			return Treasury{}, errors.New("address must be a 20-byte hex address")
		}
		holder = common.HexToAddress(evmAddr)
		if holder == (common.Address{}) {
			return Treasury{}, errors.New("address is the zero address")
		}
		out.Address = holder.Hex()
	}
	if strings.TrimSpace(algoAddr) != "" {
		if !validAlgo(algoAddr) {
			return Treasury{}, errors.New("algorand address must be 58 uppercase base32 characters")
		}
		out.Algorand = algoAddr
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	chains := Chains(cfg.Network)
	rows := make([]TokenBalance, 0, len(chains)*2)
	var mu sync.Mutex
	var wg sync.WaitGroup
	if holder != (common.Address{}) {
		for _, chain := range chains {
			if chain.Family != "evm" {
				continue
			}
			wg.Add(1)
			go func(chain Chain) {
				defer wg.Done()
				got := readEVM(ctx, caller, chain, holder)
				mu.Lock()
				rows = append(rows, got...)
				mu.Unlock()
			}(chain)
		}
	}
	if out.Algorand != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			algo, _ := ChainByID(cfg.Network, algoChainID(cfg.Network))
			row := readAlgo(ctx, httpClient, algo, out.Algorand)
			mu.Lock()
			rows = append(rows, row)
			mu.Unlock()
		}()
	}
	wg.Wait()
	// Stable order for the UI and for tests.
	ordered := make([]TokenBalance, 0, len(rows))
	for _, chain := range chains {
		for _, row := range rows {
			if row.ChainID == chain.ID {
				ordered = append(ordered, row)
			}
		}
	}
	sum := big.NewInt(0)
	for _, row := range ordered {
		if row.Error != "" {
			out.Partial = true
			continue
		}
		if row.IncludedInUSDC {
			n, ok := new(big.Int).SetString(row.Micros, 10)
			if ok {
				sum.Add(sum, n)
			}
		}
	}
	out.Balances = ordered
	out.WalletUSDC = FormatUSDC(sum)
	if holder != (common.Address{}) {
		lines, total, gerr := readGateway(ctx, httpClient, cfg, holder)
		out.GatewayLines = lines
		out.GatewayUSDC = FormatUSDC(total)
		if gerr != nil {
			out.GatewayError = gerr.Error()
			out.Partial = true
		}
	}
	return out, nil
}

func algoChainID(n Network) string {
	if n == Mainnet {
		return "algorand"
	}
	return "algorand-testnet"
}

func readEVM(ctx context.Context, caller Caller, chain Chain, holder common.Address) []TokenBalance {
	var rows []TokenBalance
	usdc, err := ERC20Balance(ctx, caller, chain, common.HexToAddress(chain.USDC), holder)
	row := TokenBalance{ChainID: chain.ID, Chain: chain.Name, Symbol: "USDC"}
	if err != nil {
		row.Error = err.Error()
		row.Amount = "0"
		row.Micros = "0"
	} else {
		row.Amount = FormatUSDC(usdc)
		row.Micros = usdc.String()
		row.IncludedInUSDC = true
	}
	rows = append(rows, row)
	if chain.EURC != "" {
		eurc, err := ERC20Balance(ctx, caller, chain, common.HexToAddress(chain.EURC), holder)
		e := TokenBalance{ChainID: chain.ID, Chain: chain.Name, Symbol: "EURC"}
		if err != nil {
			e.Error = err.Error()
			e.Amount = "0"
			e.Micros = "0"
		} else {
			e.Amount = FormatUSDC(eurc)
			e.Micros = eurc.String()
		}
		rows = append(rows, e)
	}
	return rows
}

func readAlgo(ctx context.Context, client *http.Client, chain Chain, addr string) TokenBalance {
	row := TokenBalance{ChainID: chain.ID, Chain: chain.Name, Symbol: "USDC", Amount: "0", Micros: "0"}
	url := strings.TrimRight(chain.AlgoIndexer, "/") + "/v2/accounts/" + addr
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		row.Error = err.Error()
		return row
	}
	resp, err := client.Do(req)
	if err != nil {
		row.Error = err.Error()
		return row
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		row.Error = err.Error()
		return row
	}
	if resp.StatusCode == http.StatusNotFound {
		row.IncludedInUSDC = true
		return row
	}
	if resp.StatusCode != http.StatusOK {
		row.Error = fmt.Sprintf("algorand indexer http %d", resp.StatusCode)
		return row
	}
	micros, err := ParseAlgoUSDC(body, chain.AlgoAsset)
	if err != nil {
		row.Error = err.Error()
		return row
	}
	row.Amount = FormatUSDC(micros)
	row.Micros = micros.String()
	row.IncludedInUSDC = true
	return row
}

// ParseAlgoUSDC reads one ASA holding out of an indexer account payload.
func ParseAlgoUSDC(body []byte, asset uint64) (*big.Int, error) {
	var parsed struct {
		Account struct {
			Assets []struct {
				AssetID uint64 `json:"asset-id"`
				Amount  uint64 `json:"amount"`
			} `json:"assets"`
		} `json:"account"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	for _, a := range parsed.Account.Assets {
		if a.AssetID == asset {
			return new(big.Int).SetUint64(a.Amount), nil
		}
	}
	return big.NewInt(0), nil
}

func readGateway(ctx context.Context, client *http.Client, cfg Config, holder common.Address) ([]TokenBalance, *big.Int, error) {
	chains := Chains(cfg.Network)
	var sources []map[string]any
	for _, c := range chains {
		if c.Gateway() {
			sources = append(sources, map[string]any{"domain": c.Domain, "depositor": holder.Hex()})
		}
	}
	payload, _ := json.Marshal(map[string]any{"token": "USDC", "sources": sources})
	host := cfg.GatewayURL
	if host == "" {
		host = GatewayHost(cfg.Network != Mainnet)
	}
	url := strings.TrimRight(host, "/") + "/v1/balances"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, big.NewInt(0), err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, big.NewInt(0), err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, big.NewInt(0), err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, big.NewInt(0), fmt.Errorf("gateway balances http %d", resp.StatusCode)
	}
	lines, total, err := ParseGatewayBalances(body)
	return lines, total, err
}

// ParseGatewayBalances accepts Circle's balances array. Amounts are USDC
// base units. Lines are not added to wallet balances by the caller.
func ParseGatewayBalances(body []byte) ([]TokenBalance, *big.Int, error) {
	var parsed struct {
		Balances []struct {
			Domain  int    `json:"domain"`
			Balance string `json:"balance"`
		} `json:"balances"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, big.NewInt(0), err
	}
	total := big.NewInt(0)
	var lines []TokenBalance
	for _, b := range parsed.Balances {
		// Circle's Gateway API returns a decimal USDC string, not base units.
		n, err := ParseUSDCMicros(b.Balance)
		if err != nil {
			return lines, total, fmt.Errorf("gateway balance for domain %d: %v", b.Domain, err)
		}
		total.Add(total, n)
		lines = append(lines, TokenBalance{
			ChainID: fmt.Sprintf("domain-%d", b.Domain),
			Chain:   fmt.Sprintf("Gateway domain %d", b.Domain),
			Symbol:  "USDC",
			Amount:  FormatUSDC(n),
			Micros:  n.String(),
		})
	}
	return lines, total, nil
}

// FetchAttestation reads one Iris message for a burn transaction.
func FetchAttestation(ctx context.Context, client *http.Client, src Chain, irisHost, txHash string) (message, attestation, status string, err error) {
	if !strings.HasPrefix(txHash, "0x") || len(txHash) != 66 {
		return "", "", "", errors.New("transaction hash must be 32 bytes")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	if irisHost == "" {
		irisHost = IrisHost(src.Testnet)
	}
	url := fmt.Sprintf("%s/v2/messages/%d?transactionHash=%s", strings.TrimRight(irisHost, "/"), src.Domain, txHash)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", "", err
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", "", "pending", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("iris http %d", resp.StatusCode)
	}
	return ParseIris(body)
}

// ParseIris extracts the first attested message. status is "pending" until
// an attestation is present.
func ParseIris(body []byte) (message, attestation, status string, err error) {
	var parsed struct {
		Messages []struct {
			Message     string `json:"message"`
			Attestation string `json:"attestation"`
			Status      string `json:"status"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", "", err
	}
	if len(parsed.Messages) == 0 {
		return "", "", "pending", nil
	}
	msg := parsed.Messages[0]
	if msg.Attestation == "" || msg.Attestation == "PENDING" || strings.EqualFold(msg.Status, "pending") {
		return msg.Message, "", "pending", nil
	}
	return msg.Message, msg.Attestation, "complete", nil
}

// CompleteBridge submits receiveMessage once Iris has attested the burn.
func CompleteBridge(ctx context.Context, caller Caller, client *http.Client, cfg Config, srcID, dstID, txHash string) (Result, error) {
	if err := cfg.requireOperator(ctx); err != nil {
		return Result{}, err
	}
	src, err := resolve(cfg, srcID)
	if err != nil {
		return Result{}, err
	}
	dst, err := resolve(cfg, dstID)
	if err != nil {
		return Result{}, err
	}
	if !src.CCTP() || !dst.CCTP() {
		return Result{}, errors.New("both chains must be CCTP domains")
	}
	message, attestation, status, err := FetchAttestation(ctx, client, src, cfg.IrisURL, txHash)
	if err != nil {
		return Result{}, err
	}
	res := Result{Status: status, Op: "bridge", Chain: src.ID, DestChain: dst.ID, TxHash: txHash, Explorer: src.TxURL(txHash)}
	if status != "complete" {
		res.Note = "Iris has not attested this burn yet. Complete the bridge again once the status is complete."
		return res, nil
	}
	if !cfg.WalletConfigured() {
		return res, errors.New("arc spending key is not configured")
	}
	msg, err := decodeHex(message)
	if err != nil {
		return Result{}, err
	}
	att, err := decodeHex(attestation)
	if err != nil {
		return Result{}, err
	}
	data, err := packReceive(msg, att)
	if err != nil {
		return Result{}, err
	}
	hash, err := SendContract(ctx, caller, dst, cfg.Key, common.HexToAddress(dst.MessageTransmitter), data)
	if err != nil {
		if hash == "" {
			return res, err
		}
		res.Status = "pending"
		res.TxHash = hash
		res.Explorer = dst.TxURL(hash)
		if !errors.Is(err, ErrSubmissionUncertain) {
			err = fmt.Errorf("%w: %w", ErrSubmissionUncertain, err)
		}
		return res, err
	}
	mintStatus, receiptErr := ReceiptStatus(ctx, caller, dst, hash)
	res.Status = mintStatus
	res.TxHash = hash
	res.Explorer = dst.TxURL(hash)
	res.Note = "Mint submitted on the destination chain."
	return res, receiptErr
}

func validAlgo(s string) bool {
	if len(s) != 58 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7')) {
			return false
		}
	}
	return true
}

// Diagram is the architecture the treasury page and the docs both describe.
func Diagram() map[string]any {
	return map[string]any{
		"title":  "AgentMesh on Arc",
		"bounty": "Nanopayment infrastructure shipped into an existing AI agent or API monetization tool",
		"nodes": []map[string]string{
			{"id": "canvas", "label": "Workflow canvas"},
			{"id": "arc-node", "label": "Arc node"},
			{"id": "api", "label": "Go treasury API"},
			{"id": "eip3009", "label": "EIP-3009 nanopay on Arc"},
			{"id": "cctp", "label": "CCTP burn and mint"},
			{"id": "gateway", "label": "Gateway unified balance"},
			{"id": "algorand", "label": "Algorand x402, unchanged"},
		},
		"edges": []map[string]string{
			{"from": "canvas", "to": "arc-node"},
			{"from": "arc-node", "to": "api"},
			{"from": "api", "to": "eip3009"},
			{"from": "api", "to": "cctp"},
			{"from": "api", "to": "gateway"},
			{"from": "canvas", "to": "algorand"},
		},
	}
}
