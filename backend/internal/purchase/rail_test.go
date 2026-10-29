package purchase

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type railFixture struct {
	o       Order
	receipt rpcReceipt
	logs    []rpcLog
	used    bool
	stamp   int64
	sends   int
	filters []map[string]any
}

func (f *railFixture) Call(_ context.Context, _ string, method string, args []any, out any) error {
	var value any
	switch method {
	case "eth_chainId":
		value = fmt.Sprintf("0x%x", ChainID)
	case "eth_getCode":
		if strings.EqualFold(args[0].(string), Asset) {
			value = "0x6000"
		} else {
			value = "0x"
		}
	case "eth_blockNumber":
		value = "0xa"
	case "eth_getBlockByNumber":
		value = rpcBlock{Hash: "0x" + strings.Repeat("b", 64), Number: args[0].(string), Timestamp: fmt.Sprintf("0x%x", f.stamp)}
	case "eth_getTransactionReceipt":
		value = f.receipt
	case "eth_getLogs":
		f.filters = append(f.filters, args[0].(map[string]any))
		value = f.logs
	case "eth_getTransactionCount":
		value = "0x7"
	case "eth_estimateGas":
		value = "0x186a0"
	case "eth_gasPrice":
		value = "0x1"
	case "eth_sendRawTransaction":
		f.sends++
		return errors.New("lost response")
	case "eth_call":
		data := args[0].(map[string]string)["data"]
		switch data[:10] {
		case "0x" + hex.EncodeToString(selector("decimals()")):
			value = "0x6"
		case "0x" + hex.EncodeToString(selector("DOMAIN_SEPARATOR()")):
			value = "0x361191522483d32a83e70ae7183b4b9629442c13a78bc9921d6f707911c8c6b0"
		case "0x" + hex.EncodeToString(selector("TRANSFER_WITH_AUTHORIZATION_TYPEHASH()")):
			value = "0x7c7c6cdb67a18743f49ec6fa9b35f50d52ed05cbed4cc592e13b44501c1a2267"
		case "0x" + hex.EncodeToString(selector("authorizationState(address,bytes32)")):
			if f.used {
				value = "0x1"
			} else {
				value = "0x0"
			}
		default:
			return errors.New("unexpected contract call")
		}
	default:
		return fmt.Errorf("unexpected method %s", method)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func newRailFixture(t *testing.T) (*ArcRail, *railFixture, Order) {
	t.Helper()
	o := protocolOrder(t)
	o.AuthorizationBlock = "0x5"
	o.TxHash = "0x" + strings.Repeat("c", 64)
	f := &railFixture{o: o, used: true, stamp: time.Now().Unix()}
	hash := "0x" + strings.Repeat("a", 64)
	block := "0x" + strings.Repeat("b", 64)
	used := rpcLog{Address: Asset, Topics: []string{usedTopic, addressTopic(o.Intent.Payer), o.IntentHash}, Data: "0x", TxHash: hash, BlockHash: block, BlockNumber: "0x8", Index: "0x1"}
	value, _ := new(big.Int).SetString(o.Intent.AmountMicros, 10)
	transfer := rpcLog{Address: Asset, Topics: []string{transferTopic, addressTopic(o.Intent.Payer), addressTopic(o.Intent.Payee)}, Data: "0x" + hex.EncodeToString(word(value)), TxHash: hash, BlockHash: block, BlockNumber: "0x8", Index: "0x2"}
	f.logs = []rpcLog{used}
	f.receipt = rpcReceipt{Status: "0x1", TxHash: hash, BlockHash: block, BlockNumber: "0x8", Logs: []rpcLog{used, transfer}}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return &ArcRail{Caller: f, RPC: "https://controlled.invalid", Key: key, MaxGasWei: big.NewInt(10000000), Confirmations: 2}, f, o
}

func TestArcRailReconcilesAnotherRelayerFromStoredAnchor(t *testing.T) {
	r, f, o := newRailFixture(t)
	result, err := r.Reconcile(t.Context(), o)
	if err != nil || result.Event == nil || result.Event.TxHash == o.TxHash || result.Event.LogIndex != "2" {
		t.Fatalf("external reconciliation: %+v %v", result, err)
	}
	if len(f.filters) != 1 || f.filters[0]["fromBlock"] != "0x5" || f.filters[0]["toBlock"] != "0x9" || f.sends != 0 {
		t.Fatal("reconciliation did not honor anchor or sent a replacement")
	}
	if err = r.VerifyEvent(t.Context(), o, *result.Event); err != nil {
		t.Fatal(err)
	}
}

func TestArcRailRejectsUnconfirmedOrMismatchedPaymentFacts(t *testing.T) {
	for name, change := range map[string]func(*railFixture){
		"reverted":     func(f *railFixture) { f.receipt.Status = "0x0" },
		"unconfirmed":  func(f *railFixture) { f.receipt.BlockNumber = "0xa" },
		"wrong amount": func(f *railFixture) { f.receipt.Logs[1].Data = "0x1" },
		"wrong payee": func(f *railFixture) {
			f.receipt.Logs[1].Topics[2] = addressTopic("0x3333333333333333333333333333333333333333")
		},
		"wrong authorization": func(f *railFixture) { f.receipt.Logs[0].Topics[2] = "0x" + strings.Repeat("d", 64) },
		"removed":             func(f *railFixture) { f.receipt.Logs[1].Removed = true },
		"reorg":               func(f *railFixture) { f.receipt.BlockHash = "0x" + strings.Repeat("d", 64) },
		"duplicate transfer":  func(f *railFixture) { f.receipt.Logs = append(f.receipt.Logs, f.receipt.Logs[1]) },
	} {
		t.Run(name, func(t *testing.T) {
			r, f, o := newRailFixture(t)
			change(f)
			result, err := r.Reconcile(t.Context(), o)
			if err == nil || result.Event != nil || f.sends != 0 {
				t.Fatal("uncertain payment accepted or repeated")
			}
		})
	}
}

func TestArcRailUnusedExpiryAndConsumedWithoutEvidence(t *testing.T) {
	r, f, o := newRailFixture(t)
	f.logs = nil
	f.stamp = Deadline(o).Add(time.Second).Unix()
	if _, err := r.Reconcile(t.Context(), o); !errors.Is(err, ErrUncertain) {
		t.Fatal("consumed nonce without exact receipt was released")
	}
	f.used = false
	result, err := r.Reconcile(t.Context(), o)
	if err != nil || !result.ExpiredUnused || result.Event != nil {
		t.Fatalf("confirmed unused expiry: %+v %v", result, err)
	}
}

func TestArcRailPersistsLocallyComputableTransactionIdentity(t *testing.T) {
	r, f, o := newRailFixture(t)
	prepared, err := r.Prepare(t.Context(), o, signOrder(t, o))
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := prepared.Sign(9)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	var tx types.Transaction
	if err = tx.UnmarshalBinary(encoded); err != nil || tx.Hash().Hex() != hash || tx.Nonce() != 9 || tx.ChainId().Int64() != ChainID || !strings.EqualFold(tx.To().Hex(), Asset) {
		t.Fatal("signed transaction differs from persisted identity")
	}
	if f.sends != 0 {
		t.Fatal("preparation broadcast before journal commit")
	}
	if err = r.Broadcast(t.Context(), raw, hash); !errors.Is(err, ErrUncertain) || f.sends != 1 {
		t.Fatal("lost response was not uncertain")
	}
}
