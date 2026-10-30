package purchasesim

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const transferSignature = "transferWithAuthorization(address,address,uint256,uint256,uint256,bytes32,uint8,bytes32,bytes32)"
const authorizationType = "TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)"

var usedTopic = crypto.Keccak256Hash([]byte("AuthorizationUsed(address,bytes32)"))
var transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

type authorization struct {
	From, To             common.Address
	Value, After, Before *big.Int
	Nonce                common.Hash
}

type transaction struct {
	Tx            *types.Transaction
	Sender        common.Address
	Authorization authorization
	Receipt       *types.Receipt
	ExactTransfer bool
}

func word(n *big.Int) []byte              { return common.LeftPadBytes(n.Bytes(), 32) }
func addressWord(a common.Address) []byte { return common.LeftPadBytes(a.Bytes(), 32) }
func selector(name string) []byte         { return crypto.Keccak256([]byte(name))[:4] }
func authKey(a authorization) string      { return a.From.Hex() + ":" + a.Nonce.Hex() }

func domainSeparator() common.Hash {
	return crypto.Keccak256Hash(crypto.Keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")), crypto.Keccak256([]byte("USDC")), crypto.Keccak256([]byte("2")), word(big.NewInt(ArcChainID)), addressWord(common.HexToAddress(Asset)))
}

func (f *Fixture) decodeAuthorization(data []byte) (authorization, error) {
	var a authorization
	if len(data) != 4+9*32 || !bytes.Equal(data[:4], selector(transferSignature)) {
		return a, errors.New("unsupported authorization calldata")
	}
	args := data[4:]
	if !bytes.Equal(args[:12], make([]byte, 12)) || !bytes.Equal(args[32:44], make([]byte, 12)) {
		return a, errParams
	}
	a.From, a.To = common.BytesToAddress(args[:32]), common.BytesToAddress(args[32:64])
	a.Value, a.After, a.Before = new(big.Int).SetBytes(args[64:96]), new(big.Int).SetBytes(args[96:128]), new(big.Int).SetBytes(args[128:160])
	a.Nonce = common.BytesToHash(args[160:192])
	now := f.config.Now().Unix()
	if a.From == (common.Address{}) || a.To != common.HexToAddress(f.config.Payee) || a.Value.Cmp(f.price) != 0 || !a.After.IsUint64() || !a.Before.IsUint64() || now < 0 || uint64(now) <= a.After.Uint64() || uint64(now) >= a.Before.Uint64() {
		return authorization{}, errors.New("authorization terms or deadline mismatch")
	}
	vWord := new(big.Int).SetBytes(args[192:224])
	if !vWord.IsUint64() || (vWord.Uint64() != 27 && vWord.Uint64() != 28) {
		return authorization{}, errors.New("invalid authorization recovery value")
	}
	v := byte(vWord.Uint64() - 27)
	r, s := new(big.Int).SetBytes(args[224:256]), new(big.Int).SetBytes(args[256:288])
	if !crypto.ValidateSignatureValues(v, r, s, true) {
		return authorization{}, errors.New("invalid authorization signature values")
	}
	domain := domainSeparator()
	body := crypto.Keccak256Hash(crypto.Keccak256([]byte(authorizationType)), addressWord(a.From), addressWord(a.To), word(a.Value), word(a.After), word(a.Before), a.Nonce.Bytes())
	digest := crypto.Keccak256([]byte{0x19, 0x01}, domain.Bytes(), body.Bytes())
	sig := append(append([]byte{}, args[224:288]...), v)
	pub, err := crypto.SigToPub(digest, sig)
	if err != nil || crypto.PubkeyToAddress(*pub) != a.From {
		return authorization{}, errors.New("authorization signer mismatch")
	}
	return a, nil
}

func (f *Fixture) addBlock(timestamp uint64) *types.Header {
	var parent common.Hash
	n := uint64(0)
	if prev := f.state.Blocks[f.state.Head]; prev != nil {
		n = f.state.Head + 1
		parent = prev.Hash()
		if timestamp < prev.Time {
			timestamp = prev.Time
		}
	}
	h := &types.Header{ParentHash: parent, UncleHash: types.EmptyUncleHash, Root: fixtureHash("state"), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: new(big.Int).SetUint64(n), GasLimit: 30_000_000, Time: timestamp, BaseFee: big.NewInt(1), Extra: []byte("purchase simulation")}
	f.state.Head, f.state.Blocks[n] = n, h
	return h
}

func (f *Fixture) mine(tx *transaction, outcome string) {
	if tx.Receipt != nil {
		return
	}
	for _, prior := range f.state.Transactions {
		if prior.Sender == tx.Sender && prior.Tx.Nonce() < tx.Tx.Nonce() && prior.Receipt == nil {
			return
		}
	}
	now := uint64(f.config.Now().Unix())
	if now <= tx.Authorization.After.Uint64() || now >= tx.Authorization.Before.Uint64() {
		outcome = "reverted"
	}
	header := f.addBlock(uint64(f.config.Now().Unix()))
	hash := tx.Tx.Hash()
	status := uint64(1)
	if outcome == "reverted" {
		status = 0
	}
	receipt := &types.Receipt{Type: types.DynamicFeeTxType, Status: status, CumulativeGasUsed: 100_000, GasUsed: 100_000, TxHash: hash, BlockHash: header.Hash(), BlockNumber: new(big.Int).Set(header.Number), EffectiveGasPrice: new(big.Int).Set(tx.Tx.GasFeeCap()), Logs: []*types.Log{}}
	if status == 1 {
		f.state.Consumed[authKey(tx.Authorization)] = header.Number.Uint64()
		if outcome != "consumed_without_event" {
			a := tx.Authorization
			value := new(big.Int).Set(a.Value)
			if outcome == "wrong_transfer" {
				value.Add(value, big.NewInt(1))
			}
			receipt.Logs = []*types.Log{
				{Address: common.HexToAddress(Asset), Topics: []common.Hash{usedTopic, common.BytesToHash(addressWord(a.From)), a.Nonce}, Data: []byte{}, BlockNumber: header.Number.Uint64(), TxHash: hash, BlockHash: header.Hash(), Index: 0},
				{Address: common.HexToAddress(Asset), Topics: []common.Hash{transferTopic, common.BytesToHash(addressWord(a.From)), common.BytesToHash(addressWord(a.To))}, Data: word(value), BlockNumber: header.Number.Uint64(), TxHash: hash, BlockHash: header.Hash(), Index: 1},
			}
			tx.ExactTransfer = outcome != "wrong_transfer"
		}
	}
	delete(f.state.Reserved, authKey(tx.Authorization))
	receipt.Bloom = types.CreateBloom(receipt)
	tx.Receipt = receipt
	// Keep two following blocks so the command can exercise confirmation policy.
	f.addBlock(header.Time)
	f.addBlock(header.Time)
}

func (f *Fixture) acceptRaw(raw, outcome string) (string, error) {
	data, err := hexutil.Decode(raw)
	if err != nil || len(data) > 64*1024 {
		return "", errors.New("invalid signed transaction encoding")
	}
	var tx types.Transaction
	if tx.UnmarshalBinary(data) != nil || tx.Type() != types.DynamicFeeTxType || tx.ChainId().Cmp(big.NewInt(ArcChainID)) != 0 || tx.To() == nil || *tx.To() != common.HexToAddress(Asset) || tx.Value().Sign() != 0 || tx.Gas() < 100_000 || tx.Gas() > 2_000_000 || tx.GasFeeCap().Sign() <= 0 || tx.GasTipCap().Sign() < 0 || tx.GasTipCap().Cmp(tx.GasFeeCap()) > 0 {
		return "", errors.New("signed transaction envelope mismatch")
	}
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(ArcChainID)), &tx)
	if err != nil {
		return "", errors.New("invalid transaction signature")
	}
	hash := tx.Hash()
	if _, exists := f.state.Transactions[hash]; exists {
		return hash.Hex(), nil
	}
	a, err := f.decodeAuthorization(tx.Data())
	if err != nil {
		return "", err
	}
	if tx.Nonce() != f.state.AccountNonces[sender] {
		return "", errors.New("transaction nonce mismatch")
	}
	key := authKey(a)
	if _, consumed := f.state.Consumed[key]; consumed {
		return "", errors.New("authorization already consumed")
	}
	if _, reserved := f.state.Reserved[key]; reserved {
		return "", errors.New("authorization has a pending transaction")
	}
	seenAuth := false
	for _, prior := range f.state.Transactions {
		if authKey(prior.Authorization) == key {
			seenAuth = true
			break
		}
	}
	record := &transaction{Tx: &tx, Sender: sender, Authorization: a}
	f.state.Transactions[hash] = record
	f.state.Order = append(f.state.Order, hash)
	f.state.AccountNonces[sender]++
	f.state.Reserved[key] = hash
	f.obs.TransactionHashes = append(f.obs.TransactionHashes, hash.Hex())
	if !seenAuth {
		f.obs.AuthorizationNonces = append(f.obs.AuthorizationNonces, a.Nonce.Hex())
	}
	if outcome != "pending" {
		f.mine(record, outcome)
	}
	if outcome == "lost_response" {
		return "", errors.New("simulation lost response after accepting transaction")
	}
	return hash.Hex(), nil
}

// SubmitExternal accepts a locally signed transaction supplied by the simulation
// command. It neither owns a key nor counts this as the application's broadcast.
func (f *Fixture) SubmitExternal(rawSignedHex string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.obs.ExternalAttempts++
	before := len(f.state.Transactions)
	outcome := "none"
	if f.faults.RPC == "consumed_without_event" {
		outcome = f.faults.RPC
	}
	hash, err := f.acceptRaw(rawSignedHex, outcome)
	if err == nil && len(f.state.Transactions) > before {
		f.obs.ExternalTransactionHashes = append(f.obs.ExternalTransactionHashes, hash)
	}
	return hash, err
}

func (f *Fixture) arcRPC(method string, params []json.RawMessage) (any, error) {
	switch method {
	case "eth_chainId":
		if f.faults.RPC == "wrong_chain" {
			return "0x1", nil
		}
		return hexQuantity(ArcChainID), nil
	case "eth_blockNumber":
		return hexQuantity(f.state.Head), nil
	case "eth_gasPrice":
		return "0x3b9aca00", nil
	case "eth_getCode", "eth_getTransactionCount", "eth_getBalance":
		address, err := textParam(params, 0)
		if err != nil || !common.IsHexAddress(address) || len(params) != 2 {
			return nil, errParams
		}
		tag, err := textParam(params, 1)
		block, blockErr := blockTag(tag, f.state.Head)
		if err != nil || blockErr != nil || block > f.state.Head {
			return nil, errParams
		}
		a := common.HexToAddress(address)
		if method == "eth_getCode" {
			if a == common.HexToAddress(Asset) {
				return "0x6000", nil
			}
			return "0x", nil
		}
		if method == "eth_getBalance" {
			return "0x3635c9adc5dea00000", nil
		}
		if tag == "pending" {
			return hexQuantity(f.state.AccountNonces[a]), nil
		}
		nonce := uint64(0)
		for _, tx := range f.state.Transactions {
			if tx.Sender == a && tx.Receipt != nil && tx.Receipt.BlockNumber.Uint64() <= block && tx.Tx.Nonce() >= nonce {
				nonce = tx.Tx.Nonce() + 1
			}
		}
		return hexQuantity(nonce), nil
	case "eth_call", "eth_estimateGas":
		return f.contractCall(method, params)
	case "eth_sendRawTransaction":
		f.obs.SendAttempts++
		if len(params) != 1 {
			return nil, errParams
		}
		raw, err := textParam(params, 0)
		if err != nil {
			return nil, err
		}
		return f.acceptRaw(raw, f.faults.RPC)
	case "eth_getTransactionByHash", "eth_getTransactionReceipt":
		hash, err := textParam(params, 0)
		if err != nil || len(params) != 1 || !isHash(hash) {
			return nil, errParams
		}
		tx := f.state.Transactions[common.HexToHash(hash)]
		if tx != nil && method == "eth_getTransactionByHash" {
			return map[string]any{"hash": tx.Tx.Hash().Hex()}, nil
		}
		if tx == nil || tx.Receipt == nil {
			return nil, nil
		}
		return tx.Receipt, nil
	case "eth_getBlockByNumber":
		tag, err := textParam(params, 0)
		var full bool
		if err != nil || len(params) != 2 || json.Unmarshal(params[1], &full) != nil || full {
			return nil, errParams
		}
		n, err := blockTag(tag, f.state.Head)
		if err != nil {
			return nil, err
		}
		header := f.state.Blocks[n]
		if header == nil {
			return nil, nil
		}
		encoded, err := json.Marshal(header)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err := json.Unmarshal(encoded, &out); err != nil {
			return nil, err
		}
		out["hash"], out["transactions"], out["uncles"] = header.Hash().Hex(), []string{}, []string{}
		for _, hash := range f.state.Order {
			tx := f.state.Transactions[hash]
			if tx.Receipt != nil && tx.Receipt.BlockNumber.Uint64() == n {
				out["transactions"] = append(out["transactions"].([]string), hash.Hex())
			}
		}
		return out, nil
	case "eth_getLogs":
		return f.filteredLogs(params)
	default:
		return nil, errors.New("unsupported simulation RPC method")
	}
}

func abiString(s string) string {
	b := append(word(big.NewInt(32)), word(big.NewInt(int64(len(s))))...)
	b = append(b, common.RightPadBytes([]byte(s), (len(s)+31)/32*32)...)
	return hexutil.Encode(b)
}

func (f *Fixture) contractCall(method string, params []json.RawMessage) (any, error) {
	if len(params) < 1 || len(params) > 2 {
		return nil, errParams
	}
	var call struct{ To, From, Data, Input, Value string }
	if json.Unmarshal(params[0], &call) != nil || !common.IsHexAddress(call.To) || common.HexToAddress(call.To) != common.HexToAddress(Asset) {
		return nil, errParams
	}
	if call.Data == "" {
		call.Data = call.Input
	} else if call.Input != "" && call.Input != call.Data {
		return nil, errParams
	}
	data, err := hexutil.Decode(call.Data)
	if err != nil || len(data) < 4 {
		return nil, errParams
	}
	if call.Value != "" && call.Value != "0x0" {
		return nil, errParams
	}
	block := f.state.Head
	if len(params) == 2 {
		tag, err := textParam(params, 1)
		if err != nil {
			return nil, err
		}
		block, err = blockTag(tag, f.state.Head)
		if err != nil || block > f.state.Head {
			return nil, errParams
		}
	}
	if method == "eth_estimateGas" {
		if !common.IsHexAddress(call.From) {
			return nil, errParams
		}
		a, err := f.decodeAuthorization(data)
		if err != nil {
			return nil, err
		}
		if _, used := f.state.Consumed[authKey(a)]; used {
			return nil, errors.New("authorization already consumed")
		}
		return "0x186a0", nil
	}
	for name, value := range map[string]string{"name()": abiString("USDC"), "version()": abiString("2"), "decimals()": hexutil.Encode(word(big.NewInt(6))), "TRANSFER_WITH_AUTHORIZATION_TYPEHASH()": crypto.Keccak256Hash([]byte(authorizationType)).Hex()} {
		if len(data) == 4 && bytes.Equal(data, selector(name)) {
			return value, nil
		}
	}
	if len(data) == 4 && bytes.Equal(data, selector("DOMAIN_SEPARATOR()")) {
		if f.faults.RPC == "wrong_domain" {
			return fixtureHash("wrong-domain").Hex(), nil
		}
		return domainSeparator().Hex(), nil
	}
	if len(data) == 68 && bytes.Equal(data[:4], selector("authorizationState(address,bytes32)")) && bytes.Equal(data[4:16], make([]byte, 12)) {
		key := common.BytesToAddress(data[4:36]).Hex() + ":" + common.BytesToHash(data[36:68]).Hex()
		value := int64(0)
		if at, ok := f.state.Consumed[key]; ok && at <= block {
			value = 1
		}
		return hexutil.Encode(word(big.NewInt(value))), nil
	}
	return nil, errors.New("unsupported simulation contract call")
}

func (f *Fixture) filteredLogs(params []json.RawMessage) (any, error) {
	if len(params) != 1 {
		return nil, errParams
	}
	var filter struct {
		Address json.RawMessage   `json:"address"`
		Topics  []json.RawMessage `json:"topics"`
		From    string            `json:"fromBlock"`
		To      string            `json:"toBlock"`
		Hash    string            `json:"blockHash"`
	}
	d := json.NewDecoder(bytes.NewReader(params[0]))
	d.DisallowUnknownFields()
	if d.Decode(&filter) != nil || len(filter.Topics) > 4 {
		return nil, errParams
	}
	from, to := uint64(0), f.state.Head
	var err error
	if filter.From != "" {
		from, err = blockTag(filter.From, f.state.Head)
		if err != nil {
			return nil, err
		}
	}
	if filter.To != "" {
		to, err = blockTag(filter.To, f.state.Head)
		if err != nil {
			return nil, err
		}
	}
	if from > to || (filter.Hash != "" && (!isHash(filter.Hash) || filter.From != "" || filter.To != "")) {
		return nil, errParams
	}
	addresses := []string{}
	if len(filter.Address) > 0 {
		var one string
		if json.Unmarshal(filter.Address, &one) == nil {
			addresses = []string{one}
		} else if json.Unmarshal(filter.Address, &addresses) != nil || len(addresses) == 0 {
			return nil, errParams
		}
		for _, a := range addresses {
			if !common.IsHexAddress(a) {
				return nil, errParams
			}
		}
	}
	topics := make([][]common.Hash, len(filter.Topics))
	for i, raw := range filter.Topics {
		if string(raw) == "null" {
			continue
		}
		var one string
		choices := []string{}
		if json.Unmarshal(raw, &one) == nil {
			choices = []string{one}
		} else if json.Unmarshal(raw, &choices) != nil {
			return nil, errParams
		}
		for _, choice := range choices {
			if !isHash(choice) {
				return nil, errParams
			}
			topics[i] = append(topics[i], common.HexToHash(choice))
		}
	}
	logs := []*types.Log{}
	for _, hash := range f.state.Order {
		tx := f.state.Transactions[hash]
		if tx.Receipt == nil {
			continue
		}
		for _, log := range tx.Receipt.Logs {
			if log.BlockNumber < from || log.BlockNumber > to || (filter.Hash != "" && log.BlockHash != common.HexToHash(filter.Hash)) || len(topics) > len(log.Topics) {
				continue
			}
			match := len(addresses) == 0
			for _, a := range addresses {
				if log.Address == common.HexToAddress(a) {
					match = true
				}
			}
			for i, choices := range topics {
				matched := len(choices) == 0
				for _, choice := range choices {
					if log.Topics[i] == choice {
						matched = true
					}
				}
				match = match && matched
			}
			if match {
				logs = append(logs, log)
			}
		}
	}
	return logs, nil
}
