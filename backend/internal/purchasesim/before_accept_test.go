package purchasesim

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestBeforeAcceptPreservesLedgerAndRetriesSameSignedBytes(t *testing.T) {
	f, _ := testFixture(t)
	seed := testAuthorization(t, f, 1)
	seedRaw, seedHash := signedTransaction(t, packAuthorization(t, seed), 2, 0, nil)
	result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", seedRaw))
	setFaults(t, f, Faults{RPC: "before_accept"})
	if !f.Capabilities().Features["rpc.before_accept"] {
		t.Fatal("before-acceptance capability missing")
	}
	before := f.Snapshot().state
	a := testAuthorization(t, f, 2)
	raw, hash := signedTransaction(t, packAuthorization(t, a), 2, 1, nil)
	for attempt := 1; attempt <= 2; attempt++ {
		response := rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw)
		if response.Error == nil || response.Error.Message != "simulation rejection before transaction acceptance" {
			t.Fatal("valid signed send did not hit controlled rejection")
		}
		o := f.Observations()
		if o.SendAttempts != attempt || o.RPCCalls["eth_sendRawTransaction"] != attempt || len(o.TransactionHashes) != 0 || len(o.AuthorizationNonces) != 0 || o.UniqueTransactions != 1 || o.UniqueAuthorizations != 1 || o.ConsumedAuthorizations != 1 {
			t.Fatal("rejected attempt changed acceptance observations")
		}
		if !reflect.DeepEqual(before, f.Snapshot().state) {
			t.Fatal("rejected attempt changed ledger state")
		}
	}
	if result[*types.Receipt](t, rpc(t, f.ArcHandler(), "eth_getTransactionReceipt", hash.Hex())) != nil {
		t.Fatal("rejected transaction has a receipt")
	}
	sender := crypto.PubkeyToAddress(testKey(t, 2).PublicKey).Hex()
	for _, tag := range []string{"pending", "latest"} {
		if result[string](t, rpc(t, f.ArcHandler(), "eth_getTransactionCount", sender, tag)) != "0x1" {
			t.Fatal("rejected transaction advanced sender nonce")
		}
	}
	call := append(crypto.Keccak256([]byte("authorizationState(address,bytes32)"))[:4], common.LeftPadBytes(a.From.Bytes(), 32)...)
	call = append(call, a.Nonce.Bytes()...)
	if result[string](t, rpc(t, f.ArcHandler(), "eth_call", map[string]string{"to": Asset, "data": hexutil.Encode(call)}, "latest")) != hexutil.Encode(make([]byte, 32)) {
		t.Fatal("rejected authorization nonce was consumed")
	}
	filter := map[string]any{"address": Asset, "topics": []string{usedTopic.Hex(), common.BytesToHash(common.LeftPadBytes(a.From.Bytes(), 32)).Hex(), a.Nonce.Hex()}}
	if len(result[[]*types.Log](t, rpc(t, f.ArcHandler(), "eth_getLogs", filter))) != 0 {
		t.Fatal("rejected authorization emitted an event")
	}
	invalid := rpc(t, f.ArcHandler(), "eth_sendRawTransaction", "0x01")
	if invalid.Error == nil || invalid.Error.Message == "simulation rejection before transaction acceptance" {
		t.Fatal("fault bypassed signed transaction validation")
	}
	next := testAuthorization(t, f, 3)
	nextRaw, nextHash := signedTransaction(t, packAuthorization(t, next), 2, 2, nil)
	gap := rpc(t, f.ArcHandler(), "eth_sendRawTransaction", nextRaw)
	if gap.Error == nil || gap.Error.Message != "transaction nonce mismatch" {
		t.Fatal("nonce gap bypassed validation under fault")
	}
	setFaults(t, f, Faults{})
	if !reflect.DeepEqual(before, f.Snapshot().state) || f.Observations().SendAttempts != 0 {
		t.Fatal("clearing fault changed ledger or kept attempt window")
	}
	gap = rpc(t, f.ArcHandler(), "eth_sendRawTransaction", nextRaw)
	if gap.Error == nil || gap.Error.Message != "transaction nonce mismatch" || !reflect.DeepEqual(before, f.Snapshot().state) {
		t.Fatal("nonce N+1 accepted before rejected nonce N")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw)) != hash.Hex() {
			t.Fatal("same signed bytes returned another hash")
		}
	}
	o := f.Observations()
	if o.UniqueTransactions != 2 || o.UniqueAuthorizations != 2 || o.ConsumedAuthorizations != 2 || len(o.TransactionHashes) != 1 || o.TransactionHashes[0] != hash.Hex() {
		t.Fatal("identical retry did not accept exactly once")
	}
	if receipt := result[*types.Receipt](t, rpc(t, f.ArcHandler(), "eth_getTransactionReceipt", hash.Hex())); receipt == nil || receipt.Status != 1 || receipt.TxHash != hash || len(receipt.Logs) != 2 {
		t.Fatal("accepted retry lacks payment evidence")
	}
	if result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", nextRaw)) != nextHash.Hex() {
		t.Fatal("contiguous next nonce did not accept")
	}
	if o = f.Observations(); o.SendAttempts != 4 || o.UniqueTransactions != 3 || o.ConsumedAuthorizations != 3 || len(o.TransactionHashes) != 2 {
		t.Fatal("contiguous recovery inflated unique payment count")
	}
	if receipt := result[*types.Receipt](t, rpc(t, f.ArcHandler(), "eth_getTransactionReceipt", seedHash.Hex())); receipt == nil || receipt.Status != 1 {
		t.Fatal("preexisting payment disappeared during fault recovery")
	}
}
