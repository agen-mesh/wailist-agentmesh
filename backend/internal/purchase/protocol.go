package purchase

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func Amount(s string) (*big.Int, error) {
	if len(s) == 0 || len(s) > 19 || (len(s) > 1 && s[0] == '0') {
		return nil, fmt.Errorf("invalid integer amount")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("invalid integer amount")
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || !n.IsInt64() {
		return nil, fmt.Errorf("amount exceeds supported limit")
	}
	return n, nil
}

func Address(s string) (string, error) {
	if !common.IsHexAddress(s) || common.HexToAddress(s) == (common.Address{}) {
		return "", fmt.Errorf("invalid address")
	}
	return strings.ToLower(common.HexToAddress(s).Hex()), nil
}

func Digest(b []byte) string { h := sha256.Sum256(b); return "0x" + hex.EncodeToString(h[:]) }

func CanonicalRequest(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 65536 {
		return nil, fmt.Errorf("request size outside supported range")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, fmt.Errorf("request JSON: %w", err)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("invalid request JSON")
	}
	return json.Marshal(value)
}

func NewIntent(o Order, wallet string, max string, now time.Time) (Intent, string, error) {
	payer, err := Address(wallet)
	if err != nil {
		return Intent{}, "", err
	}
	payee, err := Address(o.Offer.Payee)
	if err != nil {
		return Intent{}, "", err
	}
	owner, err := Address(o.Offer.Owner)
	if err != nil || owner != payee {
		return Intent{}, "", fmt.Errorf("offer payee must own entity")
	}
	price, err := Amount(o.Offer.PriceMicros)
	if err != nil || price.Sign() <= 0 {
		return Intent{}, "", fmt.Errorf("invalid offer price")
	}
	limit, err := Amount(max)
	if err != nil || limit.Cmp(price) < 0 {
		return Intent{}, "", fmt.Errorf("price exceeds approved maximum")
	}
	q := o.Quote
	if err := matchesEnabledChain(o.Offer.ChainID, o.Offer.Asset); err != nil || q.ChainID != o.Offer.ChainID || !strings.EqualFold(q.Asset, o.Offer.Asset) || !strings.EqualFold(q.Payee, payee) || q.AmountMicros != o.Offer.PriceMicros || q.Method != "POST" || o.Offer.Method != "POST" || q.Resource != o.Offer.Endpoint || q.RequestDigest != Digest(o.Request) || q.ID == "" || q.Proof == "" {
		return Intent{}, "", fmt.Errorf("merchant challenge does not match offer and request")
	}
	deadline := q.Deadline.UTC().Truncate(time.Second)
	if !deadline.After(now.Add(30*time.Second)) || deadline.After(now.Add(15*time.Minute)) || deadline.After(o.Offer.ValidUntil) {
		return Intent{}, "", fmt.Errorf("invalid challenge deadline")
	}
	expires, ok := new(big.Int).SetString(o.Offer.ExpiresAtBlock, 10)
	if !ok {
		return Intent{}, "", fmt.Errorf("invalid native expiry")
	}
	observed, ok := new(big.Int).SetString(o.Offer.ObservedBlock, 10)
	if !ok || expires.Cmp(observed) <= 0 {
		return Intent{}, "", fmt.Errorf("offer expired at native block")
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return Intent{}, "", err
	}
	quoteBytes, err := json.Marshal(q)
	if err != nil {
		return Intent{}, "", err
	}
	i := Intent{Version: Protocol, OrderID: o.ID, AccountID: o.AccountID, WorkflowID: o.Intent.WorkflowID, RunID: o.Intent.RunID, NodeID: o.Intent.NodeID, Invocation: o.Intent.Invocation, OfferKey: o.Offer.Key, OfferOwner: owner, OfferVersion: o.Offer.Version, OfferContentHash: o.Offer.ContentHash, OfferSnapshotHash: o.Offer.SnapshotHash, Payer: payer, Payee: payee, ChainID: o.Offer.ChainID, Asset: common.HexToAddress(o.Offer.Asset).Hex(), AmountMicros: o.Offer.PriceMicros, MaxMicros: max, Method: "POST", Resource: q.Resource, RequestDigest: q.RequestDigest, QuoteDigest: Digest(quoteBytes), OrderSalt: "0x" + hex.EncodeToString(salt), ValidAfter: strconv.FormatInt(now.Add(-30*time.Second).Unix(), 10), ValidBefore: strconv.FormatInt(deadline.Unix(), 10)}
	b, err := json.Marshal(i)
	if err != nil {
		return Intent{}, "", err
	}
	return i, Digest(b), nil
}

func Deadline(o Order) time.Time {
	n, _ := strconv.ParseInt(o.Intent.ValidBefore, 10, 64)
	return time.Unix(n, 0).UTC()
}

func Authorization(o Order, signature string) (arc.Authorization, error) {
	b, err := json.Marshal(o.Intent)
	if err != nil || Digest(b) != o.IntentHash {
		return arc.Authorization{}, ErrDenied
	}
	value, err := Amount(o.Intent.AmountMicros)
	if err != nil {
		return arc.Authorization{}, err
	}
	after, ok := new(big.Int).SetString(o.Intent.ValidAfter, 10)
	if !ok {
		return arc.Authorization{}, ErrDenied
	}
	before, ok := new(big.Int).SetString(o.Intent.ValidBefore, 10)
	if !ok {
		return arc.Authorization{}, ErrDenied
	}
	a := arc.Authorization{From: common.HexToAddress(o.Intent.Payer), To: common.HexToAddress(o.Intent.Payee), Value: value, ValidAfter: after, ValidBefore: before, Nonce: common.HexToHash(o.IntentHash)}
	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	if err != nil || len(sig) != 65 {
		return a, ErrDenied
	}
	v := sig[64]
	if v >= 27 {
		v -= 27
	}
	if !crypto.ValidateSignatureValues(v, new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), true) {
		return a, ErrDenied
	}
	a.V = v + 27
	copy(a.R[:], sig[:32])
	copy(a.S[:], sig[32:64])
	if err := matchesEnabledChain(o.Intent.ChainID, o.Intent.Asset); err != nil {
		return a, ErrDenied
	}
	signer, err := arc.RecoverAuthorizationSigner("USDC", "2", o.Intent.ChainID, common.HexToAddress(o.Intent.Asset), a)
	if err != nil || signer != a.From {
		return a, ErrDenied
	}
	return a, nil
}

func VerifyWallet(c WalletChallenge, signature string, now time.Time) error {
	if !now.Before(c.ExpiresAt) {
		return ErrDenied
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	if err != nil || len(sig) != 65 {
		return ErrDenied
	}
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	if !crypto.ValidateSignatureValues(sig[64], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), true) {
		return ErrDenied
	}
	pub, err := crypto.SigToPub(accounts.TextHash([]byte(c.Message)), sig)
	if err != nil || !strings.EqualFold(crypto.PubkeyToAddress(*pub).Hex(), c.Address) {
		return ErrDenied
	}
	return nil
}

func TypedData(o Order) map[string]any {
	field := func(name, kind string) map[string]string { return map[string]string{"name": name, "type": kind} }
	return map[string]any{"domain": map[string]any{"name": "USDC", "version": "2", "chainId": o.Intent.ChainID, "verifyingContract": o.Intent.Asset}, "types": map[string]any{"EIP712Domain": []any{field("name", "string"), field("version", "string"), field("chainId", "uint256"), field("verifyingContract", "address")}, "TransferWithAuthorization": []any{field("from", "address"), field("to", "address"), field("value", "uint256"), field("validAfter", "uint256"), field("validBefore", "uint256"), field("nonce", "bytes32")}}, "primaryType": "TransferWithAuthorization", "message": map[string]any{"from": o.Intent.Payer, "to": o.Intent.Payee, "value": o.Intent.AmountMicros, "validAfter": o.Intent.ValidAfter, "validBefore": o.Intent.ValidBefore, "nonce": o.IntentHash}}
}

func View(o Order, now time.Time) map[string]any {
	freshIdentity := o.IdentityVerifiedAt != nil && o.IdentityValidUntil != nil && now.Before(*o.IdentityValidUntil)
	v := map[string]any{"id": o.ID, "intentHash": o.IntentHash, "state": o.State, "offer": o.Offer, "payer": o.Intent.Payer, "payee": o.Intent.Payee, "chainId": o.Intent.ChainID, "asset": o.Intent.Asset, "amountMicros": o.Intent.AmountMicros, "maxMicros": o.Intent.MaxMicros, "method": o.Intent.Method, "resource": o.Intent.Resource, "requestDigest": o.Intent.RequestDigest, "quoteDigest": o.Intent.QuoteDigest, "deadline": Deadline(o), "refundPolicy": RefundPolicy, "canApprove": o.State == AwaitingApproval && freshIdentity && now.Before(Deadline(o)), "canAuthorize": o.State == Approved && freshIdentity && now.Before(Deadline(o).Add(-10*time.Second)), "canCancel": o.SignatureReleasedAt == nil && (o.State == AwaitingIdentity || o.State == AwaitingApproval || o.State == Approved), "canReconcile": o.SignatureReleasedAt != nil && o.State != Completed && o.State != Expired}
	if o.IdentityVerifiedAt != nil {
		v["identityVerifiedAt"] = o.IdentityVerifiedAt
	}
	if o.ApprovedAt != nil {
		v["approvedAt"] = o.ApprovedAt
	}
	if o.SignatureReleasedAt != nil {
		v["signatureReleasedAt"] = o.SignatureReleasedAt
	}
	if o.TxHash != "" {
		v["txHash"] = o.TxHash
	}
	if o.PaymentEvent != nil {
		v["paymentEvent"] = o.PaymentEvent
	}
	if len(o.Result) > 0 {
		v["result"] = o.Result
	}
	if o.Reason != "" {
		v["reason"] = o.Reason
	}
	return v
}
