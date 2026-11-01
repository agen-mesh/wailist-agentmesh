package arc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/testutil"
)

func TestArcSubmissionOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, method, message, receipt string
		httpStatus                     int
		uncertain, hash                bool
	}{
		{name: "estimate fails before send", method: "eth_estimateGas", message: "fixture estimate failure"},
		{name: "definitive rejection", method: "eth_sendRawTransaction", message: "insufficient funds for gas * price + value"},
		{name: "nonce already consumed", method: "eth_sendRawTransaction", message: "nonce too low", uncertain: true, hash: true},
		{name: "already known", method: "eth_sendRawTransaction", message: "already known", uncertain: true, hash: true},
		{name: "unclassified rpc error", method: "eth_sendRawTransaction", message: "upstream response unavailable", uncertain: true, hash: true},
		{name: "http rejection is not inclusion proof", method: "eth_sendRawTransaction", httpStatus: 400, uncertain: true, hash: true},
		{name: "confirmed revert", receipt: "0x0", hash: true},
		{name: "malformed receipt status", receipt: "unknown", uncertain: true, hash: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := policyConfig()
			fixture := &testutil.ArcRPC{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Method string `json:"method"`
					Params []any  `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if request.Method == tc.method {
					if tc.httpStatus != 0 {
						w.WriteHeader(tc.httpStatus)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -32000, "message": tc.message}})
					return
				}
				var result any
				if request.Method == "eth_getTransactionReceipt" && tc.receipt != "" {
					result = map[string]string{"status": tc.receipt}
				} else if err := fixture.Call(r.Context(), "", request.Method, request.Params, &result); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
			}))
			defer server.Close()
			caller := redirectedCaller{Caller: HTTPCaller{HTTP: server.Client()}, url: server.URL}
			result, err := Execute(WithPrincipal(context.Background(), "owner"), caller, cfg, Request{
				Op: "transfer", ChainID: "arc-testnet", To: cfg.Address.Hex(), Amount: "0.01", Broadcast: true,
			})
			if err == nil || errors.Is(err, ErrSubmissionUncertain) != tc.uncertain {
				t.Fatalf("uncertain=%v want=%v result=%+v err=%v", errors.Is(err, ErrSubmissionUncertain), tc.uncertain, result, err)
			}
			if (result.TxHash != "") != tc.hash {
				t.Fatalf("hash present=%v want=%v result=%+v", result.TxHash != "", tc.hash, result)
			}
			if tc.uncertain && result.Status != "pending" {
				t.Fatalf("uncertain result status=%s", result.Status)
			}
			if !tc.hash && fixture.Sends.Load() != 0 {
				t.Fatalf("refused action accepted %d transactions", fixture.Sends.Load())
			}
		})
	}
}

type redirectedCaller struct {
	Caller
	url string
}

func (c redirectedCaller) Call(ctx context.Context, _ string, method string, params []any, out any) error {
	return c.Caller.Call(ctx, c.url, method, params, out)
}

func TestArcSubmissionConfirmedApprovalSurvivesUnsentTransfer(t *testing.T) {
	cfg := policyConfig()
	fixture := &testutil.ArcRPC{}
	caller := &approvalThenFailure{fixture: fixture}
	result, err := Execute(WithPrincipal(context.Background(), "owner"), caller, cfg, Request{
		Op: "bridge", ChainID: "arc-testnet", DestChainID: "base-sepolia", Amount: "0.01", To: cfg.Address.Hex(), Broadcast: true,
	})
	if err == nil || errors.Is(err, ErrSubmissionUncertain) || result.TxHash != "" || result.ApproveTx == "" || result.ApproveTx != fixture.Hash.Load() {
		t.Fatalf("confirmed approval lost or marked uncertain: %+v err=%v", result, err)
	}
	if fixture.Sends.Load() != 1 || !strings.Contains(err.Error(), "fixture before second send") {
		t.Fatalf("sends=%d err=%v", fixture.Sends.Load(), err)
	}
}

type approvalThenFailure struct{ fixture *testutil.ArcRPC }

func (c *approvalThenFailure) Call(ctx context.Context, url, method string, params []any, out any) error {
	if method == "eth_estimateGas" && c.fixture.Sends.Load() == 1 {
		return errors.New("fixture before second send")
	}
	return c.fixture.Call(ctx, url, method, params, out)
}
