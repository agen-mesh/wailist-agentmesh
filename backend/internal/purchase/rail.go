package purchase

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type ArcRail struct {
	Caller        arc.Caller
	RPC           string
	Key           *ecdsa.PrivateKey
	MaxGasWei     *big.Int
	Confirmations uint64
}

func hexInt(s string) (*big.Int, error) {
	if !strings.HasPrefix(s, "0x") || len(s) < 3 {
		return nil, fmt.Errorf("invalid RPC integer")
	}
	n, ok := new(big.Int).SetString(s[2:], 16)
	if !ok || n.Sign() < 0 {
		return nil, fmt.Errorf("invalid RPC integer")
	}
	return n, nil
}
func hexUint(s string) (uint64, error) {
	n, err := hexInt(s)
	if err != nil {
		return 0, err
	}
	if !n.IsUint64() {
		return 0, fmt.Errorf("RPC integer overflow")
	}
	return n.Uint64(), nil
}
func word(n *big.Int) []byte      { b := make([]byte, 32); n.FillBytes(b); return b }
func addressWord(a string) []byte { return common.LeftPadBytes(common.HexToAddress(a).Bytes(), 32) }
func selector(s string) []byte    { return crypto.Keccak256([]byte(s))[:4] }
func (r *ArcRail) call(ctx context.Context, method string, args []any, out any) error {
	if r.Caller == nil || r.RPC == "" {
		return ErrDisabled
	}
	return r.Caller.Call(ctx, r.RPC, method, args, out)
}
func (r *ArcRail) read(ctx context.Context, data []byte, block string) (string, error) {
	var out string
	err := r.call(ctx, "eth_call", []any{map[string]string{"to": Asset, "data": "0x" + hex.EncodeToString(data)}, block}, &out)
	return out, err
}

func (r *ArcRail) Check(ctx context.Context) error {
	var chain, code string
	if err := r.call(ctx, "eth_chainId", nil, &chain); err != nil {
		return err
	}
	n, err := hexUint(chain)
	if err != nil || n != uint64(ChainID) {
		return fmt.Errorf("unexpected purchase chain")
	}
	if err = r.call(ctx, "eth_getCode", []any{Asset, "latest"}, &code); err != nil {
		return err
	}
	if code == "0x" || code == "0x0" || code == "" {
		return fmt.Errorf("purchase token is not a contract")
	}
	decimals, err := r.read(ctx, selector("decimals()"), "latest")
	if err != nil {
		return err
	}
	d, err := hexUint(decimals)
	if err != nil || d != 6 {
		return fmt.Errorf("purchase token decimals changed")
	}
	domain := crypto.Keccak256Hash(crypto.Keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")), crypto.Keccak256([]byte("USDC")), crypto.Keccak256([]byte("2")), word(big.NewInt(ChainID)), addressWord(Asset))
	got, err := r.read(ctx, selector("DOMAIN_SEPARATOR()"), "latest")
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, domain.Hex()) {
		return fmt.Errorf("purchase token domain mismatch")
	}
	typehash, err := r.read(ctx, selector("TRANSFER_WITH_AUTHORIZATION_TYPEHASH()"), "latest")
	if err != nil {
		return err
	}
	expected := crypto.Keccak256Hash([]byte("TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)"))
	if !strings.EqualFold(typehash, expected.Hex()) {
		return fmt.Errorf("purchase authorization type mismatch")
	}
	return nil
}
func (r *ArcRail) Anchor(ctx context.Context) (string, error) {
	var block string
	if err := r.call(ctx, "eth_blockNumber", nil, &block); err != nil {
		return "", err
	}
	if _, err := hexUint(block); err != nil {
		return "", err
	}
	return block, nil
}
func (r *ArcRail) IsEOA(ctx context.Context, address string) error {
	if _, err := Address(address); err != nil {
		return err
	}
	var code string
	if err := r.call(ctx, "eth_getCode", []any{address, "latest"}, &code); err != nil {
		return err
	}
	if code != "0x" && code != "0x0" {
		return fmt.Errorf("purchase wallet must be an externally owned account")
	}
	return nil
}

func (r *ArcRail) Prepare(ctx context.Context, o Order, signature string) (Prepared, error) {
	if r.Key == nil || r.MaxGasWei == nil || r.MaxGasWei.Sign() <= 0 {
		return Prepared{}, ErrDisabled
	}
	if err := r.Check(ctx); err != nil {
		return Prepared{}, err
	}
	a, err := Authorization(o, signature)
	if err != nil {
		return Prepared{}, err
	}
	data, err := arc.AuthorizationData(a)
	if err != nil {
		return Prepared{}, err
	}
	from := crypto.PubkeyToAddress(r.Key.PublicKey)
	var nonceHex, gasHex, priceHex string
	if err = r.call(ctx, "eth_getTransactionCount", []any{from.Hex(), "pending"}, &nonceHex); err != nil {
		return Prepared{}, err
	}
	nonce, err := hexUint(nonceHex)
	if err != nil {
		return Prepared{}, err
	}
	if err = r.call(ctx, "eth_estimateGas", []any{map[string]string{"from": from.Hex(), "to": Asset, "data": "0x" + hex.EncodeToString(data), "value": "0x0"}}, &gasHex); err != nil {
		return Prepared{}, fmt.Errorf("authorization simulation rejected")
	}
	gas, err := hexInt(gasHex)
	if err != nil {
		return Prepared{}, err
	}
	gas.Mul(gas, big.NewInt(12))
	gas.Div(gas, big.NewInt(10))
	if !gas.IsUint64() || gas.Sign() <= 0 || gas.Uint64() > 2_000_000 {
		return Prepared{}, fmt.Errorf("relay gas outside limit")
	}
	if err = r.call(ctx, "eth_gasPrice", nil, &priceHex); err != nil {
		return Prepared{}, err
	}
	price, err := hexInt(priceHex)
	if err != nil || price.Sign() <= 0 {
		return Prepared{}, fmt.Errorf("invalid gas price")
	}
	price.Mul(price, big.NewInt(2))
	if new(big.Int).Mul(gas, price).Cmp(r.MaxGasWei) > 0 {
		return Prepared{}, fmt.Errorf("relay gas budget exceeded")
	}
	return Prepared{Relayer: strings.ToLower(from.Hex()), ObservedNonce: nonce, Sign: func(n uint64) (string, string, error) {
		to := common.HexToAddress(Asset)
		tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(ChainID), Nonce: n, GasTipCap: price, GasFeeCap: price, Gas: gas.Uint64(), To: &to, Value: big.NewInt(0), Data: data})
		signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(ChainID)), r.Key)
		if err != nil {
			return "", "", err
		}
		raw, err := signed.MarshalBinary()
		if err != nil {
			return "", "", err
		}
		return "0x" + hex.EncodeToString(raw), signed.Hash().Hex(), nil
	}}, nil
}
func (r *ArcRail) Broadcast(ctx context.Context, raw, hash string) error {
	b, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	if err != nil || crypto.Keccak256Hash(b).Hex() != hash {
		return ErrDenied
	}
	var got string
	if err = r.call(ctx, "eth_sendRawTransaction", []any{raw}, &got); err != nil {
		return ErrUncertain
	}
	if !strings.EqualFold(got, hash) {
		return ErrUncertain
	}
	return nil
}

func (r *ArcRail) Recover(ctx context.Context, o Order, raw, signature string) (bool, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	var tx types.Transaction
	if err != nil || tx.UnmarshalBinary(b) != nil || tx.Hash().Hex() != o.TxHash || tx.ChainId().Cmp(big.NewInt(ChainID)) != 0 || tx.To() == nil || *tx.To() != common.HexToAddress(Asset) || tx.Value().Sign() != 0 {
		return false, ErrDenied
	}
	a, err := Authorization(o, signature)
	if err != nil {
		return false, ErrDenied
	}
	data, err := arc.AuthorizationData(a)
	if err != nil || !bytes.Equal(data, tx.Data()) {
		return false, ErrDenied
	}
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(ChainID)), &tx)
	if err != nil {
		return false, ErrDenied
	}
	if err = r.Check(ctx); err != nil {
		return false, err
	}
	confirmed, _, err := r.confirmed(ctx)
	if err != nil {
		return false, err
	}
	var count string
	if err = r.call(ctx, "eth_getTransactionCount", []any{sender.Hex(), fmt.Sprintf("0x%x", confirmed)}, &count); err != nil {
		return false, err
	}
	nonce, err := hexUint(count)
	if err != nil {
		return false, err
	}
	if nonce > tx.Nonce() {
		return true, nil
	}
	var known *struct {
		Hash string `json:"hash"`
	}
	if err = r.call(ctx, "eth_getTransactionByHash", []any{o.TxHash}, &known); err != nil {
		return false, err
	}
	if known != nil {
		if !strings.EqualFold(known.Hash, o.TxHash) {
			return false, ErrUncertain
		}
		return false, nil
	}
	if err = r.call(ctx, "eth_getTransactionCount", []any{sender.Hex(), "latest"}, &count); err != nil {
		return false, err
	}
	nonce, err = hexUint(count)
	if err != nil {
		return false, err
	}
	if nonce > tx.Nonce() {
		return false, nil
	}
	if nonce < tx.Nonce() {
		return false, ErrUncertain
	}
	// Even after authorization expiry, these exact bytes can consume the missing
	// sender nonce through a reverted transaction without a replacement payment.
	return false, r.Broadcast(ctx, raw, o.TxHash)
}

type rpcLog struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	TxHash      string   `json:"transactionHash"`
	BlockHash   string   `json:"blockHash"`
	BlockNumber string   `json:"blockNumber"`
	Index       string   `json:"logIndex"`
	Removed     bool     `json:"removed"`
}
type rpcReceipt struct {
	Status      string   `json:"status"`
	TxHash      string   `json:"transactionHash"`
	BlockHash   string   `json:"blockHash"`
	BlockNumber string   `json:"blockNumber"`
	Logs        []rpcLog `json:"logs"`
}
type rpcBlock struct {
	Hash      string `json:"hash"`
	Number    string `json:"number"`
	Timestamp string `json:"timestamp"`
}

var usedTopic = crypto.Keccak256Hash([]byte("AuthorizationUsed(address,bytes32)")).Hex()
var transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()

func addressTopic(a string) string { return "0x" + hex.EncodeToString(addressWord(a)) }
func logMatches(l rpcLog, topics ...string) bool {
	if l.Removed || !strings.EqualFold(l.Address, Asset) || len(l.Topics) != len(topics) {
		return false
	}
	for i, t := range topics {
		if !strings.EqualFold(l.Topics[i], t) {
			return false
		}
	}
	return true
}

func (r *ArcRail) confirmed(ctx context.Context) (uint64, rpcBlock, error) {
	if r.Confirmations < 1 {
		return 0, rpcBlock{}, ErrDisabled
	}
	head, err := r.Anchor(ctx)
	if err != nil {
		return 0, rpcBlock{}, err
	}
	n, err := hexUint(head)
	if err != nil || n < r.Confirmations {
		return 0, rpcBlock{}, ErrUncertain
	}
	n = n - r.Confirmations + 1
	var block *rpcBlock
	if err = r.call(ctx, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", n), false}, &block); err != nil {
		return 0, rpcBlock{}, err
	}
	if block == nil || block.Hash == "" {
		return 0, rpcBlock{}, ErrUncertain
	}
	bn, err := hexUint(block.Number)
	if err != nil || bn != n {
		return 0, rpcBlock{}, ErrUncertain
	}
	return n, *block, nil
}

func (r *ArcRail) verifyReceipt(ctx context.Context, o Order, hash string, confirmed uint64) (*PaymentEvent, error) {
	var receipt *rpcReceipt
	if err := r.call(ctx, "eth_getTransactionReceipt", []any{hash}, &receipt); err != nil {
		return nil, err
	}
	if receipt == nil {
		return nil, ErrUncertain
	}
	status, err := hexUint(receipt.Status)
	if err != nil || status != 1 {
		return nil, ErrUncertain
	}
	height, err := hexUint(receipt.BlockNumber)
	if err != nil || height > confirmed || !strings.EqualFold(receipt.TxHash, hash) {
		return nil, ErrUncertain
	}
	var block *rpcBlock
	if err = r.call(ctx, "eth_getBlockByNumber", []any{receipt.BlockNumber, false}, &block); err != nil {
		return nil, err
	}
	if block == nil || !strings.EqualFold(block.Hash, receipt.BlockHash) {
		return nil, ErrUncertain
	}
	used := 0
	var transfers []rpcLog
	for _, l := range receipt.Logs {
		if !strings.EqualFold(l.TxHash, hash) || !strings.EqualFold(l.BlockHash, receipt.BlockHash) || l.BlockNumber != receipt.BlockNumber {
			continue
		}
		if logMatches(l, usedTopic, addressTopic(o.Intent.Payer), o.IntentHash) {
			used++
		}
		if logMatches(l, transferTopic, addressTopic(o.Intent.Payer), addressTopic(o.Intent.Payee)) {
			value, err := hexInt(l.Data)
			if err == nil && value.String() == o.Intent.AmountMicros {
				transfers = append(transfers, l)
			}
		}
	}
	if used != 1 || len(transfers) != 1 {
		return nil, ErrUncertain
	}
	index, err := hexUint(transfers[0].Index)
	if err != nil {
		return nil, err
	}
	return &PaymentEvent{ChainID: ChainID, TxHash: strings.ToLower(hash), LogIndex: strconv.FormatUint(index, 10), BlockHash: strings.ToLower(receipt.BlockHash), BlockNumber: strconv.FormatUint(height, 10)}, nil
}

func (r *ArcRail) Reconcile(ctx context.Context, o Order) (Settlement, error) {
	if err := r.Check(ctx); err != nil {
		return Settlement{}, err
	}
	confirmed, block, err := r.confirmed(ctx)
	if err != nil {
		return Settlement{}, err
	}
	start, err := hexUint(o.AuthorizationBlock)
	if err != nil || start > confirmed || confirmed-start > 100_000 {
		return Settlement{}, ErrUncertain
	}
	for from := start; from <= confirmed; {
		to := from + 999
		if to > confirmed {
			to = confirmed
		}
		var logs []rpcLog
		filter := map[string]any{"address": Asset, "fromBlock": fmt.Sprintf("0x%x", from), "toBlock": fmt.Sprintf("0x%x", to), "topics": []string{usedTopic, addressTopic(o.Intent.Payer), o.IntentHash}}
		if err = r.call(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
			return Settlement{}, err
		}
		for _, l := range logs {
			if logMatches(l, usedTopic, addressTopic(o.Intent.Payer), o.IntentHash) {
				event, err := r.verifyReceipt(ctx, o, l.TxHash, confirmed)
				if err != nil {
					return Settlement{}, err
				}
				return Settlement{Event: event}, nil
			}
		}
		if to == confirmed {
			break
		}
		from = to + 1
	}
	data := append(selector("authorizationState(address,bytes32)"), addressWord(o.Intent.Payer)...)
	data = append(data, common.HexToHash(o.IntentHash).Bytes()...)
	state, err := r.read(ctx, data, fmt.Sprintf("0x%x", confirmed))
	if err != nil {
		return Settlement{}, err
	}
	used, err := hexUint(state)
	if err != nil || used != 0 {
		return Settlement{}, ErrUncertain
	}
	timestamp, err := hexUint(block.Timestamp)
	if err != nil {
		return Settlement{}, err
	}
	deadline, err := strconv.ParseUint(o.Intent.ValidBefore, 10, 64)
	if err != nil {
		return Settlement{}, err
	}
	return Settlement{ExpiredUnused: timestamp > deadline}, nil
}

func (r *ArcRail) VerifyEvent(ctx context.Context, o Order, event PaymentEvent) error {
	if err := r.Check(ctx); err != nil {
		return err
	}
	confirmed, _, err := r.confirmed(ctx)
	if err != nil {
		return err
	}
	actual, err := r.verifyReceipt(ctx, o, event.TxHash, confirmed)
	if err != nil {
		return err
	}
	if *actual != event {
		return ErrDenied
	}
	return nil
}
