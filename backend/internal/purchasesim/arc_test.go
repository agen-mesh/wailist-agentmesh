package purchasesim

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestSignedTransactionRejectsInvalidTerms(t *testing.T) {
	cases := []struct {
		name     string
		auth     func(*arc.Authorization)
		domain   string
		envelope func(*types.DynamicFeeTx)
		data     func([]byte) []byte
	}{
		{name: "positive"},
		{name: "outer_chain", envelope: func(tx *types.DynamicFeeTx) { tx.ChainID = big.NewInt(1) }},
		{name: "outer_token", envelope: func(tx *types.DynamicFeeTx) {
			a := common.HexToAddress("0x1111111111111111111111111111111111111111")
			tx.To = &a
		}},
		{name: "native_value", envelope: func(tx *types.DynamicFeeTx) { tx.Value = big.NewInt(1) }},
		{name: "nonce_gap", envelope: func(tx *types.DynamicFeeTx) { tx.Nonce = 2 }},
		{name: "payee", auth: func(a *arc.Authorization) { a.To = common.HexToAddress("0x3333333333333333333333333333333333333333") }},
		{name: "price", auth: func(a *arc.Authorization) { a.Value = big.NewInt(999999) }},
		{name: "payer", auth: func(a *arc.Authorization) { a.From = common.HexToAddress("0x3333333333333333333333333333333333333333") }},
		{name: "deadline", auth: func(a *arc.Authorization) { a.ValidBefore = big.NewInt(1900000000) }},
		{name: "not_yet_valid", auth: func(a *arc.Authorization) { a.ValidAfter = big.NewInt(1900000000) }},
		{name: "bad_recovery", auth: func(a *arc.Authorization) { a.V = 0 }},
		{name: "high_s", auth: func(a *arc.Authorization) {
			a.S = common.BigToHash(new(big.Int).Sub(crypto.S256().Params().N, a.S.Big()))
			a.V = 27 + (1 - (a.V - 27))
		}},
		{name: "wrong_domain", domain: "1"},
		{name: "truncated", data: func(b []byte) []byte { return b[:len(b)-1] }},
		{name: "trailing_bytes", data: func(b []byte) []byte { return append(b, 0) }},
		{name: "address_padding", data: func(b []byte) []byte { b[4] = 1; return b }},
		{name: "selector", data: func(b []byte) []byte { b[0] ^= 1; return b }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := testFixture(t)
			a := testAuthorization(t, f, 1)
			if tc.domain != "" {
				var err error
				a, err = arc.SignAuthorization(testKey(t, 1), "USDC", tc.domain, ArcChainID, common.HexToAddress(Asset), a)
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.auth != nil {
				tc.auth(&a)
			}
			data := packAuthorization(t, a)
			if tc.data != nil {
				data = tc.data(data)
			}
			raw, hash := signedTransaction(t, data, 2, 0, tc.envelope)
			r := rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw)
			if tc.name == "positive" {
				if result[string](t, r) != hash.Hex() {
					t.Fatal("hash differs from signed bytes")
				}
			} else {
				requireError(t, r)
				if f.Observations().UniqueTransactions != 0 || f.Observations().ConsumedAuthorizations != 0 {
					t.Fatal("invalid payment mutated ledger")
				}
			}
		})
	}
}

func TestReceiptReplayAndFilteredEvidence(t *testing.T) {
	f, _ := testFixture(t)
	a := testAuthorization(t, f, 1)
	raw, hash := signedTransaction(t, packAuthorization(t, a), 2, 0, nil)
	for i := 0; i < 2; i++ {
		if result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw)) != hash.Hex() {
			t.Fatal("retry hash mismatch")
		}
	}
	r := result[*types.Receipt](t, rpc(t, f.ArcHandler(), "eth_getTransactionReceipt", hash.Hex()))
	if r == nil || r.Status != 1 || r.TxHash != hash || len(r.Logs) != 2 || !bytes.Equal(r.Logs[1].Data, common.LeftPadBytes(a.Value.Bytes(), 32)) {
		t.Fatal("canonical payment evidence missing")
	}
	block := result[map[string]any](t, rpc(t, f.ArcHandler(), "eth_getBlockByNumber", hexQuantity(r.BlockNumber.Uint64()), false))
	if block["hash"] != r.BlockHash.Hex() {
		t.Fatal("receipt block is not canonical")
	}
	if o := f.Observations(); o.SendAttempts != 2 || o.UniqueTransactions != 1 || o.UniqueAuthorizations != 1 || o.ConsumedAuthorizations != 1 {
		t.Fatal("retry inflated unique payment count")
	}
	used := crypto.Keccak256Hash([]byte("AuthorizationUsed(address,bytes32)"))
	payer := common.BytesToHash(common.LeftPadBytes(a.From.Bytes(), 32))
	filters := []struct {
		name   string
		filter map[string]any
		count  int
	}{
		{"exact", map[string]any{"address": Asset, "fromBlock": hexQuantity(r.BlockNumber.Uint64()), "toBlock": "latest", "topics": []any{used.Hex(), payer.Hex(), a.Nonce.Hex()}}, 1},
		{"wrong_address", map[string]any{"address": f.config.Payee}, 0},
		{"wrong_payer", map[string]any{"topics": []any{used.Hex(), common.Hash{}.Hex()}}, 0},
		{"wrong_nonce", map[string]any{"topics": []any{used.Hex(), payer.Hex(), common.Hash{}.Hex()}}, 0},
		{"after_block", map[string]any{"fromBlock": hexQuantity(r.BlockNumber.Uint64() + 1)}, 0},
		{"before_block", map[string]any{"toBlock": hexQuantity(r.BlockNumber.Uint64() - 1)}, 0},
		{"wrong_block_hash", map[string]any{"blockHash": common.Hash{}.Hex()}, 0},
		{"block_hash", map[string]any{"blockHash": r.BlockHash.Hex()}, 2},
		{"topic_or", map[string]any{"address": []string{f.config.Payee, Asset}, "topics": []any{[]string{common.Hash{}.Hex(), used.Hex()}, nil}}, 1},
	}
	for _, tc := range filters {
		t.Run(tc.name, func(t *testing.T) {
			logs := result[[]*types.Log](t, rpc(t, f.ArcHandler(), "eth_getLogs", tc.filter))
			if len(logs) != tc.count {
				t.Fatalf("matched %d events, expected %d", len(logs), tc.count)
			}
		})
	}
	for _, filter := range []map[string]any{{"topics": []string{"0x12"}}, {"address": "0x12"}, {"fromBlock": "0x3", "toBlock": "0x2"}, {"blockHash": r.BlockHash.Hex(), "fromBlock": "0x0"}, {"ignored": true}} {
		requireError(t, rpc(t, f.ArcHandler(), "eth_getLogs", filter))
	}
	replay, _ := signedTransaction(t, packAuthorization(t, a), 3, 0, nil)
	requireError(t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", replay))
	b := testAuthorization(t, f, 2)
	second, _ := signedTransaction(t, packAuthorization(t, b), 2, 1, nil)
	result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", second))
	if f.Observations().UniqueTransactions != 2 || f.Observations().ConsumedAuthorizations != 2 {
		t.Fatal("fresh subsequent payment rejected")
	}
}

func TestFaultsAndCounterResetPreserveLedger(t *testing.T) {
	for _, fault := range []string{"lost_response", "pending", "reverted", "wrong_transfer", "consumed_without_event"} {
		t.Run(fault, func(t *testing.T) {
			f, _ := testFixture(t)
			setFaults(t, f, Faults{RPC: fault})
			a := testAuthorization(t, f, 1)
			raw, hash := signedTransaction(t, packAuthorization(t, a), 2, 0, nil)
			response := rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw)
			if fault == "lost_response" {
				requireError(t, response)
			} else {
				result[string](t, response)
			}
			r := result[*types.Receipt](t, rpc(t, f.ArcHandler(), "eth_getTransactionReceipt", hash.Hex()))
			switch fault {
			case "pending":
				if r != nil {
					t.Fatal("pending transaction has receipt")
				}
			case "reverted":
				if r == nil || r.Status != 0 || len(r.Logs) != 0 || f.Observations().ConsumedAuthorizations != 0 {
					t.Fatal("reverted transaction has payment evidence")
				}
			case "wrong_transfer":
				if r == nil || len(r.Logs) != 2 || new(big.Int).SetBytes(r.Logs[1].Data).Cmp(a.Value) == 0 {
					t.Fatal("wrong-transfer control contains exact payment")
				}
			case "consumed_without_event":
				if r == nil || r.Status != 1 || len(r.Logs) != 0 || f.Observations().ConsumedAuthorizations != 1 {
					t.Fatal("nonce-only fault inconsistent")
				}
			case "lost_response":
				if r == nil || r.Status != 1 || len(r.Logs) != 2 {
					t.Fatal("lost response did not preserve payment")
				}
			}
			setFaults(t, f, Faults{})
			if o := f.Observations(); o.SendAttempts != 0 || o.UniqueTransactions != 1 {
				t.Fatal("fault reset erased ledger or kept interval counter")
			}
			result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw))
			if f.Observations().UniqueTransactions != 1 {
				t.Fatal("same bytes created second transaction")
			}
			if fault == "pending" {
				r = result[*types.Receipt](t, rpc(t, f.ArcHandler(), "eth_getTransactionReceipt", hash.Hex()))
				if r == nil || r.Status != 1 {
					t.Fatal("pending transaction never settled")
				}
			}
			call := append(crypto.Keccak256([]byte("authorizationState(address,bytes32)"))[:4], common.LeftPadBytes(a.From.Bytes(), 32)...)
			call = append(call, a.Nonce.Bytes()...)
			state := result[string](t, rpc(t, f.ArcHandler(), "eth_call", map[string]string{"to": Asset, "data": hexutil.Encode(call)}, "latest"))
			n, ok := new(big.Int).SetString(state[2:], 16)
			if !ok || (n.Sign() == 0) != (fault == "reverted") {
				t.Fatal("authorization state mismatch")
			}
		})
	}
}

func TestExternalRelayerAndExpiredReplay(t *testing.T) {
	f, now := testFixture(t)
	a := testAuthorization(t, f, 1)
	raw, hash := signedTransaction(t, packAuthorization(t, a), 3, 0, nil)
	got, err := f.SubmitExternal(raw)
	if err != nil || got != hash.Hex() {
		t.Fatal("external signed transfer rejected")
	}
	o := f.Observations()
	if o.SendAttempts != 0 || o.ExternalAttempts != 1 || len(o.ExternalTransactionHashes) != 1 || o.ExternalTransactionHashes[0] != hash.Hex() || o.ConsumedAuthorizations != 1 {
		t.Fatal("external payment attributed to own send")
	}
	logs := result[[]*types.Log](t, rpc(t, f.ArcHandler(), "eth_getLogs", map[string]any{"address": Asset, "fromBlock": "0x100", "topics": []any{usedTopic.Hex(), common.BytesToHash(common.LeftPadBytes(a.From.Bytes(), 32)).Hex(), a.Nonce.Hex()}}))
	if len(logs) != 1 || logs[0].TxHash != hash || f.Observations().SendAttempts != 0 {
		t.Fatal("external payment cannot be discovered before own send")
	}
	*now = now.Add(time.Hour)
	if result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw)) != hash.Hex() || f.Observations().UniqueTransactions != 1 {
		t.Fatal("old identical bytes created another payment")
	}
	newBytes, _ := signedTransaction(t, packAuthorization(t, a), 2, 0, nil)
	requireError(t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", newBytes))
}

func TestNetworkAndContractReadControls(t *testing.T) {
	f, _ := testFixture(t)
	if result[string](t, rpc(t, f.ArcHandler(), "eth_chainId")) != "0x4cef52" {
		t.Fatal("wrong Arc chain")
	}
	for _, name := range []string{"decimals()", "DOMAIN_SEPARATOR()", "TRANSFER_WITH_AUTHORIZATION_TYPEHASH()", "name()", "version()"} {
		data := hexutil.Encode(crypto.Keccak256([]byte(name))[:4])
		result[string](t, rpc(t, f.ArcHandler(), "eth_call", map[string]string{"to": Asset, "data": data}, "latest"))
	}
	setFaults(t, f, Faults{RPC: "wrong_domain"})
	got := result[string](t, rpc(t, f.ArcHandler(), "eth_call", map[string]string{"to": Asset, "data": hexutil.Encode(crypto.Keccak256([]byte("DOMAIN_SEPARATOR()"))[:4])}, "latest"))
	if got == domainSeparator().Hex() {
		t.Fatal("wrong domain fault ineffective")
	}
	setFaults(t, f, Faults{RPC: "wrong_chain"})
	if result[string](t, rpc(t, f.ArcHandler(), "eth_chainId")) == hexQuantity(ArcChainID) {
		t.Fatal("wrong chain fault ineffective")
	}
}

func TestPendingNonceOrderAndMiningDeadline(t *testing.T) {
	f, now := testFixture(t)
	setFaults(t, f, Faults{RPC: "pending"})
	a := testAuthorization(t, f, 1)
	raw, first := signedTransaction(t, packAuthorization(t, a), 2, 0, nil)
	result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw))
	setFaults(t, f, Faults{RPC: "wrong_transfer"})
	b := testAuthorization(t, f, 2)
	raw, second := signedTransaction(t, packAuthorization(t, b), 2, 1, nil)
	result[string](t, rpc(t, f.ArcHandler(), "eth_sendRawTransaction", raw))
	if result[*types.Receipt](t, rpc(t, f.ArcHandler(), "eth_getTransactionReceipt", second.Hex())) != nil {
		t.Fatal("later nonce mined ahead of pending predecessor")
	}
	*now = now.Add(time.Hour)
	setFaults(t, f, Faults{})
	for _, hash := range []common.Hash{first, second} {
		r := result[*types.Receipt](t, rpc(t, f.ArcHandler(), "eth_getTransactionReceipt", hash.Hex()))
		if r == nil || r.Status != 0 || len(r.Logs) != 0 {
			t.Fatal("expired authorization mined as a payment")
		}
	}
	if f.Observations().ConsumedAuthorizations != 0 {
		t.Fatal("expired pending authorization was consumed")
	}
}
