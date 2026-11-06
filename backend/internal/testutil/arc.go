package testutil

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
)

// ArcRPC never delegates to a network transport.
type ArcRPC struct {
	Calls        atomic.Int64
	Sends        atomic.Int64
	Hash         atomic.Value
	Signer       atomic.Value
	LoseResponse bool
	RejectSend   string
	Pending      bool
	OnReceipt    func()
}

func (f *ArcRPC) Call(_ context.Context, _ string, method string, params []any, out any) error {
	f.Calls.Add(1)
	var result any = "0x1"
	switch method {
	case "eth_getTransactionCount":
		result = "0x0"
	case "eth_estimateGas":
		result = "0x186a0"
	case "eth_maxPriorityFeePerGas", "eth_gasPrice":
	case "eth_call":
		data := ""
		switch call := params[0].(type) {
		case map[string]any:
			data, _ = call["data"].(string)
		case map[string]string:
			data = call["data"]
		}
		value := ""
		if strings.HasPrefix(data, "0x06fdde03") {
			value = "USDC"
		} else if strings.HasPrefix(data, "0x54fd4d50") {
			value = "2"
		}
		if value != "" {
			typ, err := abi.NewType("string", "", nil)
			if err != nil {
				return err
			}
			encoded, err := (abi.Arguments{{Type: typ}}).Pack(value)
			if err != nil {
				return err
			}
			result = "0x" + hex.EncodeToString(encoded)
		} else {
			result = "0x" + strings.Repeat("0", 64)
		}
	case "eth_sendRawTransaction":
		raw, err := hex.DecodeString(strings.TrimPrefix(params[0].(string), "0x"))
		if err != nil {
			return err
		}
		var tx types.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil {
			return err
		}
		signer, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), &tx)
		if err != nil {
			return err
		}
		f.Sends.Add(1)
		f.Hash.Store(tx.Hash().Hex())
		f.Signer.Store(signer.Hex())
		if f.RejectSend != "" {
			return errors.New(f.RejectSend)
		}
		if f.LoseResponse {
			return errors.New("fixture: response lost after accepting transaction")
		}
		result = tx.Hash().Hex()
	case "eth_getTransactionReceipt":
		if f.OnReceipt != nil {
			f.OnReceipt()
		}
		if f.Pending {
			result = nil
		} else {
			result = map[string]string{"status": "0x1"}
		}
	default:
		return fmt.Errorf("unexpected fixture RPC method %q", method)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

func (f *ArcRPC) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/messages/") {
		f.Calls.Add(1)
		body = []byte(`{"messages":[{"message":"0x1234","attestation":"0x5678","status":"complete"}]}`)
	} else {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if r.Body == nil {
			return nil, errors.New("unexpected fixture request without a body")
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		var result any
		if err := f.Call(r.Context(), "", req.Method, req.Params, &result); err != nil {
			return nil, err
		}
		var err error
		body, err = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
		if err != nil {
			return nil, err
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

func UseArcRPC(t *testing.T, rpc *ArcRPC) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = rpc
	t.Cleanup(func() { http.DefaultTransport = old })
}
