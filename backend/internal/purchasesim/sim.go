// Package purchasesim provides explicit, in-memory test fixtures. Import it only
// from a simulation command or tests, never as a production RPC fallback.
package purchasesim

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const ArcChainID = 5042002
const ArkivChainID = 7738577
const Asset = "0x3600000000000000000000000000000000000000"

var errParams = errors.New("invalid simulation RPC parameters")

type Config struct {
	Resource, Payee, PriceMicros string
	Now                          func() time.Time
}

type Faults struct {
	RPC   string `json:"rpc"`
	Arkiv string `json:"arkiv"`
}

type Observations struct {
	RPCCalls                  map[string]int `json:"rpcCalls"`
	SendAttempts              int            `json:"sendAttempts"`
	ExternalAttempts          int            `json:"externalAttempts"`
	ExternalTransactionHashes []string       `json:"externalTransactionHashes"`
	TransactionHashes         []string       `json:"transactionHashes"`
	AuthorizationNonces       []string       `json:"authorizationNonces"`
	ArkivQueries              int            `json:"arkivQueries"`
	UniqueTransactions        int            `json:"uniqueTransactions"`
	UniqueAuthorizations      int            `json:"uniqueAuthorizations"`
	ConsumedAuthorizations    int            `json:"consumedAuthorizations"`
}

type Capabilities struct {
	EvidenceMode string          `json:"evidenceMode"`
	Features     map[string]bool `json:"features"`
}

type Fixture struct {
	mu     sync.Mutex
	config Config
	price  *big.Int
	faults Faults
	state  chainState
	obs    Observations
}

type chainState struct {
	Head          uint64
	Blocks        map[uint64]*types.Header
	Transactions  map[common.Hash]*transaction
	Order         []common.Hash
	AccountNonces map[common.Address]uint64
	Consumed      map[string]uint64
	Reserved      map[string]common.Hash
	ArkivHead     uint64
	Offers        []offerRevision
}

// StateSnapshot is an opaque in-process copy. Use MarshalState for restarts.
type StateSnapshot struct {
	identity string
	state    chainState
	faults   Faults
	obs      Observations
}

func New(config Config) (*Fixture, error) {
	u, err := url.Parse(config.Resource)
	price, ok := new(big.Int).SetString(config.PriceMicros, 10)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !common.IsHexAddress(config.Payee) || common.HexToAddress(config.Payee) == (common.Address{}) || !ok || price.Sign() <= 0 || !price.IsUint64() || price.String() != config.PriceMicros {
		return nil, errors.New("invalid purchase simulation configuration")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Now().Unix() < 10000 {
		return nil, errors.New("simulation clock must be after Unix second 10000")
	}
	config.Payee = strings.ToLower(config.Payee)
	f := &Fixture{config: config, price: price}
	f.Reset()
	return f, nil
}

func (f *Fixture) identity() string {
	return f.config.Resource + "|" + f.config.Payee + "|" + f.config.PriceMicros
}

func emptyObservations() Observations {
	return Observations{RPCCalls: map[string]int{}, TransactionHashes: []string{}, AuthorizationNonces: []string{}, ExternalTransactionHashes: []string{}}
}

func (f *Fixture) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = Faults{RPC: "none", Arkiv: "none"}
	f.obs = emptyObservations()
	f.state = chainState{Blocks: map[uint64]*types.Header{}, Transactions: map[common.Hash]*transaction{}, AccountNonces: map[common.Address]uint64{}, Consumed: map[string]uint64{}, Reserved: map[string]common.Hash{}, ArkivHead: 4096}
	for n := uint64(0); n <= 256; n++ {
		f.addBlock(uint64(f.config.Now().Unix()) - 512 + n*2)
	}
	f.reviseOffer("none")
}

var rpcFaults = []string{"none", "lost_response", "pending", "consumed_without_event", "wrong_domain", "wrong_chain", "reverted", "wrong_transfer"}
var arkivFaults = []string{"none", "changed", "expired", "empty", "changed_owner", "changed_version", "changed_payload", "native_expired", "utc_expired"}

func known(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (f *Fixture) SetFaults(faults Faults) error {
	if faults.RPC == "" {
		faults.RPC = "none"
	}
	if faults.Arkiv == "" {
		faults.Arkiv = "none"
	}
	if !known(rpcFaults, faults.RPC) || !known(arkivFaults, faults.Arkiv) {
		return errors.New("unsupported simulation fault")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.obs = emptyObservations()
	if faults.Arkiv != f.faults.Arkiv {
		f.reviseOffer(faults.Arkiv)
	}
	f.faults = faults
	if faults.RPC == "none" {
		for _, hash := range f.state.Order {
			tx := f.state.Transactions[hash]
			if tx.Receipt == nil {
				f.mine(tx, "none")
			}
		}
	}
	return nil
}

func (f *Fixture) Capabilities() Capabilities {
	features := map[string]bool{"arc.signed_dynamic_fee": true, "arc.external_relayer": true, "arc.log_filters": true, "state.restore": true, "arkiv.read": true, "arkiv.write": false, "merchant.execute": false}
	features["arc.external_consumed_without_event"] = true
	for _, name := range rpcFaults {
		features["rpc."+name] = true
	}
	for _, name := range arkivFaults {
		features["arkiv."+name] = true
	}
	return Capabilities{EvidenceMode: "simulation", Features: features}
}

func cloneObservations(in Observations) Observations {
	out := in
	out.RPCCalls = map[string]int{}
	for k, v := range in.RPCCalls {
		out.RPCCalls[k] = v
	}
	out.TransactionHashes = append([]string{}, in.TransactionHashes...)
	out.ExternalTransactionHashes = append([]string{}, in.ExternalTransactionHashes...)
	out.AuthorizationNonces = append([]string{}, in.AuthorizationNonces...)
	return out
}

func (f *Fixture) Observations() Observations {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := cloneObservations(f.obs)
	out.UniqueTransactions = len(f.state.Transactions)
	seen := map[string]bool{}
	for _, tx := range f.state.Transactions {
		seen[authKey(tx.Authorization)] = true
	}
	out.UniqueAuthorizations = len(seen)
	out.ConsumedAuthorizations = len(f.state.Consumed)
	return out
}

func cloneState(in chainState) chainState {
	out := in
	out.Blocks = map[uint64]*types.Header{}
	for k, v := range in.Blocks {
		out.Blocks[k] = types.CopyHeader(v)
	}
	out.Transactions = map[common.Hash]*transaction{}
	for k, v := range in.Transactions {
		copyTx := *v
		if v.Receipt != nil {
			receipt := *v.Receipt
			receipt.Logs = make([]*types.Log, len(v.Receipt.Logs))
			for i, log := range v.Receipt.Logs {
				copyLog := *log
				copyLog.Topics = append([]common.Hash{}, log.Topics...)
				copyLog.Data = append([]byte{}, log.Data...)
				receipt.Logs[i] = &copyLog
			}
			copyTx.Receipt = &receipt
		}
		out.Transactions[k] = &copyTx
	}
	out.Order = append([]common.Hash{}, in.Order...)
	out.AccountNonces = map[common.Address]uint64{}
	for k, v := range in.AccountNonces {
		out.AccountNonces[k] = v
	}
	out.Consumed = map[string]uint64{}
	for k, v := range in.Consumed {
		out.Consumed[k] = v
	}
	out.Reserved = map[string]common.Hash{}
	for k, v := range in.Reserved {
		out.Reserved[k] = v
	}
	out.Offers = append([]offerRevision{}, in.Offers...)
	return out
}

func (f *Fixture) Snapshot() StateSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return StateSnapshot{f.identity(), cloneState(f.state), f.faults, cloneObservations(f.obs)}
}

func (f *Fixture) Restore(snapshot StateSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if snapshot.identity != f.identity() || snapshot.state.Blocks == nil {
		return errors.New("snapshot belongs to another simulation")
	}
	f.state, f.faults, f.obs = cloneState(snapshot.state), snapshot.faults, cloneObservations(snapshot.obs)
	return nil
}

func (f *Fixture) AdvanceBlocks(arcBlocks, arkivBlocks uint64) error {
	if arcBlocks > 10000 || arkivBlocks > 10000 {
		return errParams
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := uint64(0); i < arcBlocks; i++ {
		f.addBlock(uint64(f.config.Now().Unix()))
	}
	f.state.ArkivHead += arkivBlocks
	return nil
}

type persistedState struct {
	Version  int
	Identity string
	State    chainState
	Faults   Faults
	Observed Observations
}

// MarshalState contains signed transactions. Store it privately, never in logs
// or the verification HTTP response. No signing keys are retained by Fixture.
func (f *Fixture) MarshalState() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.Marshal(persistedState{1, f.identity(), f.state, f.faults, f.obs})
	if err != nil {
		return nil, fmt.Errorf("encode simulation state: %w", err)
	}
	return data, nil
}

func (f *Fixture) RestoreState(data []byte) error {
	if len(data) > 16*1024*1024 {
		return errors.New("simulation state exceeds size limit")
	}
	var saved persistedState
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var trailing any
	if d.Decode(&saved) != nil || d.Decode(&trailing) != io.EOF || saved.Version != 1 || saved.Identity != f.identity() || !known(rpcFaults, saved.Faults.RPC) || !known(arkivFaults, saved.Faults.Arkiv) {
		return errors.New("invalid simulation state")
	}
	s := saved.State
	if s.Blocks == nil || s.Blocks[s.Head] == nil || s.Transactions == nil || s.AccountNonces == nil || s.Consumed == nil || s.Reserved == nil || saved.Observed.RPCCalls == nil || len(s.Order) != len(s.Transactions) || len(s.Offers) == 0 {
		return errors.New("incomplete simulation state")
	}
	seen := map[common.Hash]bool{}
	for _, hash := range s.Order {
		tx := s.Transactions[hash]
		if seen[hash] || tx == nil || tx.Tx == nil || tx.Tx.Hash() != hash || tx.Tx.Type() != types.DynamicFeeTxType || tx.Tx.ChainId().Cmp(big.NewInt(ArcChainID)) != 0 || tx.Authorization.Value == nil || tx.Authorization.After == nil || tx.Authorization.Before == nil {
			return errors.New("invalid simulation transaction state")
		}
		seen[hash] = true
		sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(ArcChainID)), tx.Tx)
		if err != nil || sender != tx.Sender {
			return errors.New("invalid restored transaction signer")
		}
		if tx.Receipt != nil {
			r := tx.Receipt
			if r.BlockNumber == nil || !r.BlockNumber.IsUint64() || s.Blocks[r.BlockNumber.Uint64()] == nil || r.TxHash != hash || r.BlockHash != s.Blocks[r.BlockNumber.Uint64()].Hash() {
				return errors.New("invalid restored receipt")
			}
			for _, log := range r.Logs {
				if log == nil || log.TxHash != hash || log.BlockHash != r.BlockHash || log.BlockNumber != r.BlockNumber.Uint64() {
					return errors.New("invalid restored event")
				}
			}
		}
	}
	for n, header := range s.Blocks {
		if header == nil || header.Number == nil || !header.Number.IsUint64() || header.Number.Uint64() != n || n > s.Head {
			return errors.New("invalid simulation block state")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state, f.faults, f.obs = s, saved.Faults, saved.Observed
	return nil
}

func (f *Fixture) ArcHandler() http.Handler   { return f.handler(false) }
func (f *Fixture) ArkivHandler() http.Handler { return f.handler(true) }

type rpcRequest struct {
	Version string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

type rpcResponse struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (f *Fixture) handler(arkiv bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024+1))
		if err != nil || len(body) > 1024*1024 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request rpcRequest
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&request)
		var trailing any
		if err != nil || decoder.Decode(&trailing) != io.EOF || request.Version != "2.0" || len(request.ID) == 0 || string(request.ID) == "null" || request.Method == "" {
			_ = json.NewEncoder(w).Encode(rpcResponse{Version: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32600, "invalid simulation RPC request"}})
			return
		}
		f.mu.Lock()
		f.obs.RPCCalls[request.Method]++
		var result any
		if arkiv {
			result, err = f.arkivRPC(request.Method, request.Params)
		} else {
			result, err = f.arcRPC(request.Method, request.Params)
		}
		f.mu.Unlock()
		response := rpcResponse{Version: "2.0", ID: request.ID, Result: result}
		if err != nil {
			response.Result = nil
			response.Error = &rpcError{-32000, err.Error()}
		}
		// JSON-RPC success requires an explicit result:null for pending receipts.
		if result == nil && err == nil {
			response.Result = json.RawMessage("null")
		}
		_ = json.NewEncoder(w).Encode(response)
	})
}

func textParam(params []json.RawMessage, i int) (string, error) {
	var s string
	if i >= len(params) || json.Unmarshal(params[i], &s) != nil || s == "" {
		return "", errParams
	}
	return s, nil
}

func isHash(s string) bool {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}

func blockTag(raw string, head uint64) (uint64, error) {
	if raw == "latest" || raw == "pending" || raw == "safe" || raw == "finalized" {
		return head, nil
	}
	if raw == "earliest" {
		return 0, nil
	}
	if !strings.HasPrefix(raw, "0x") || len(raw) < 3 {
		return 0, errParams
	}
	n, ok := new(big.Int).SetString(raw[2:], 16)
	if !ok || !n.IsUint64() {
		return 0, errParams
	}
	return n.Uint64(), nil
}

func hexQuantity(n uint64) string { return fmt.Sprintf("0x%x", n) }
func fixtureHash(label string) common.Hash {
	return crypto.Keccak256Hash([]byte("purchasesim:v1:" + label))
}
