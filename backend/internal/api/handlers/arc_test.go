package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api"
	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/testutil"
	"github.com/golang-jwt/jwt/v5"
)

func TestArcHTTPRunnerPrincipalHandoff(t *testing.T) {
	for _, tc := range []struct {
		name, user, mode string
		allowed          bool
	}{
		{"operator start", "owner", "start", true},
		{"member start", "dev", "start", false},
		{"operator resume", "owner", "resume", true},
		{"member forced resume", "dev", "resume", false},
		{"webhook with operator token", "owner", "webhook", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ARC_NETWORK", "testnet")
			t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
			t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
			t.Setenv("WEB_READONLY_MODE", "")
			d := testDeps(t)
			d.JWTSecret = "arc-run-route-test"
			d.Broker = sse.NewBroker()
			d.Engine = engine.NewRunner(d.Store, d.Broker, nil, "", "", "", engine.X402Config{})
			router := api.NewRouter(d)
			rpc := &testutil.ArcRPC{}
			testutil.UseArcRPC(t, rpc)
			wf, err := d.Store.CreateWorkflow(t.Context(), "Arc handoff", tc.user)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				d.Engine.Stop(wf.ID)
				if err := d.Store.DeleteWorkflow(context.Background(), wf.ID); err != nil {
					t.Error(err)
				}
			})
			wf, err = d.Store.UpdateWorkflow(t.Context(), wf.ID, wf.Name, models.WorkflowGraph{
				Nodes: []models.WorkflowNode{
					{ID: "start", Type: models.NodeTypeTrigger, Template: "webhook", Secrets: map[string]string{"webhookSecret": "fixture-secret"}},
					{ID: "pay", Type: models.NodeTypeArc, Template: "transfer", Price: "0.01", Endpoint: "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC", Config: map[string]string{"chain": "arc-testnet", "broadcast": "true", "operator": "true", "userID": "owner"}},
				},
				Edges: []models.WorkflowEdge{{ID: "a", From: "start", To: "pay", Kind: models.EdgeKindFlow}},
			})
			if err != nil {
				t.Fatal(err)
			}
			path := "/workflows/" + wf.ID + "/run"
			if tc.mode == "resume" {
				run, err := d.Store.CreateRun(t.Context(), wf.ID, "test", []byte(`{}`))
				if err != nil {
					t.Fatal(err)
				}
				if err := d.Store.FinishRun(t.Context(), run.ID, models.RunStatusFailed); err != nil {
					t.Fatal(err)
				}
				path = "/runs/" + run.ID + "/resume?force=true"
			} else if tc.mode == "webhook" {
				if err := d.Store.SetWorkflowDeployed(t.Context(), wf.ID, "fixture", time.Now()); err != nil {
					t.Fatal(err)
				}
				path = "/run/" + wf.ID
			}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"operator":true,"userID":"owner","worldVerified":true}`))
			req.Header.Set("Authorization", "Bearer "+api.TestMakeToken(d.JWTSecret, tc.user))
			req.Header.Set("X-Webhook-Secret", "fixture-secret")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusAccepted {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var response map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for d.Engine.IsRunning(wf.ID) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			run, err := d.Store.GetRun(t.Context(), response["runId"])
			want := models.RunStatusFailed
			if tc.allowed {
				want = models.RunStatusSuccess
			}
			if err != nil || run.Status != want || d.Engine.IsRunning(wf.ID) {
				t.Fatalf("run=%+v err=%v want=%s", run, err, want)
			}
			if tc.allowed && rpc.Sends.Load() != 1 || !tc.allowed && rpc.Calls.Load() != 0 {
				t.Fatalf("calls=%d sends=%d allowed=%v", rpc.Calls.Load(), rpc.Sends.Load(), tc.allowed)
			}
		})
	}
}

func TestArcChainsIsPublic(t *testing.T) {
	t.Setenv("ARC_NETWORK", "")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "")
	r := api.NewRouter(&handlers.Deps{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/arc/chains", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "5042002") {
		t.Fatalf("body missing Arc testnet chain id: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(strings.ToLower(w.Body.String()), "privatekey") {
		t.Fatal("chains response looks like it includes a key")
	}
}

func TestArcQuoteIgnoresBroadcast(t *testing.T) {
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	r := api.NewRouter(&handlers.Deps{})
	req := httptest.NewRequest(http.MethodPost, "/arc/quote", strings.NewReader(
		`{"op":"nanopay","chain":"arc-testnet","to":"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC","amount":"0.01","broadcast":true}`,
	))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"quoted"`) {
		t.Fatalf("body %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "ac0974") {
		t.Fatal("quote echoed the private key")
	}
}

func TestArcTransferRequiresASession(t *testing.T) {
	r := api.NewRouter(&handlers.Deps{})
	req := httptest.NewRequest(http.MethodPost, "/arc/transfer", strings.NewReader(`{"amount":"1","to":"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}

func TestArcChallengeWithoutAKeyIsUnavailable(t *testing.T) {
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "")
	t.Setenv("ARC_PAYTO", "")
	r := api.NewRouter(&handlers.Deps{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x402/arc", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestArcChallengeWithAKeyIs402(t *testing.T) {
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	t.Setenv("ARC_PAYTO", "")
	r := api.NewRouter(&handlers.Deps{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x402/arc?amount=0.01", nil))
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "eip155:5042002") || !strings.Contains(body, "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266") {
		t.Fatalf("body %s", body)
	}
	if strings.Contains(body, "ac0974") {
		t.Fatal("challenge included the private key")
	}
}

func TestArcRawRoutesEnforceOperatorAuthority(t *testing.T) {
	t.Setenv("ARC_NETWORK", "testnet")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	t.Setenv("WEB_READONLY_MODE", "")
	d := testDeps(t)
	d.JWTSecret = "arc-route-test"
	router := api.NewRouter(d)
	rpc := &testutil.ArcRPC{}
	testutil.UseArcRPC(t, rpc)
	body := `{"chain":"arc-testnet","destChain":"base-sepolia","to":"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC","amount":"0.01","broadcast":true,"txHash":"0x1111111111111111111111111111111111111111111111111111111111111111","role":"operator","operator":true,"userID":"owner","worldVerified":true}`
	forgedRole, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "dev", "exp": time.Now().Add(time.Hour).Unix(), "role": "operator", "operator": true, "worldVerified": true,
	}).SignedString([]byte(d.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	routes := []string{"/arc/transfer", "/arc/bridge", "/arc/bridge/complete", "/arc/gateway/deposit", "/arc/nanopay", "/x402/arc/buy", "/x402/arc"}
	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			for _, tc := range []struct {
				name, token string
				status      int
			}{
				{"anonymous", "", http.StatusUnauthorized},
				{"forged signature", api.TestMakeToken("wrong-secret", "owner"), http.StatusUnauthorized},
				{"ordinary account", api.TestMakeToken(d.JWTSecret, "dev"), http.StatusForbidden},
				{"role forgery", forgedRole, http.StatusForbidden},
			} {
				t.Run(tc.name, func(t *testing.T) {
					before := rpc.Calls.Load()
					req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
					if tc.token != "" {
						req.Header.Set("Authorization", "Bearer "+tc.token)
					}
					req.Header.Set("X-Operator-User-ID", "owner")
					req.Header.Set("X-World-Verified", "true")
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
					if w.Code != tc.status || rpc.Calls.Load() != before {
						t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body.String(), rpc.Calls.Load()-before)
					}
				})
			}
			before := rpc.Sends.Load()
			req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
			req.AddCookie(&http.Cookie{Name: "agentmesh_token", Value: api.TestMakeToken(d.JWTSecret, "owner")})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if route == "/x402/arc" {
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "recipient") || rpc.Sends.Load() != before {
					t.Fatalf("operator settlement validation: %d %s", w.Code, w.Body.String())
				}
			} else if w.Code != http.StatusOK || rpc.Sends.Load() <= before {
				t.Fatalf("operator: %d %s sends=%d", w.Code, w.Body.String(), rpc.Sends.Load()-before)
			}
		})
	}
}

func TestArcRouterPolicyIsDefaultDenyAndLoadedOnce(t *testing.T) {
	t.Setenv("ARC_OPERATOR_USER_IDS", "")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "")
	t.Setenv("WEB_READONLY_MODE", "")
	d := testDeps(t)
	d.JWTSecret = "arc-route-test"
	router := api.NewRouter(d)
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	for _, route := range []string{"/arc/transfer", "/x402/arc", "/arc/bridge/complete", "/x402/arc/buy"} {
		req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{"broadcast":true}`))
		req.Header.Set("Authorization", "Bearer "+api.TestMakeToken(d.JWTSecret, "owner"))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
	}
}

func TestArcQuoteRoutesStayReadOnlyForOrdinaryAccounts(t *testing.T) {
	t.Setenv("ARC_OPERATOR_USER_IDS", "")
	d := testDeps(t)
	d.JWTSecret = "arc-route-test"
	router := api.NewRouter(d)
	rpc := &testutil.ArcRPC{}
	testutil.UseArcRPC(t, rpc)
	for _, route := range []string{"/arc/transfer", "/arc/bridge", "/arc/gateway/deposit", "/arc/nanopay", "/arc/quote"} {
		req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{"op":"transfer","chain":"arc-testnet","destChain":"base-sepolia","to":"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC","amount":"1"}`))
		req.Header.Set("Authorization", "Bearer "+api.TestMakeToken(d.JWTSecret, "dev"))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"quoted"`) || rpc.Calls.Load() != 0 {
			t.Fatalf("%s: %d %s calls=%d", route, w.Code, w.Body.String(), rpc.Calls.Load())
		}
	}
}

func TestArcRawRoutesPreserveUncertainTransactionIdentity(t *testing.T) {
	t.Setenv("ARC_NETWORK", "testnet")
	t.Setenv("ARC_OPERATOR_USER_IDS", "owner")
	t.Setenv("ARC_SPEND_PRIVATE_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	t.Setenv("WEB_READONLY_MODE", "")
	d := testDeps(t)
	d.JWTSecret = "arc-route-test"
	router := api.NewRouter(d)
	rpc := &testutil.ArcRPC{LoseResponse: true}
	testutil.UseArcRPC(t, rpc)
	for _, route := range []string{"/arc/transfer", "/arc/bridge", "/arc/gateway/deposit", "/arc/nanopay", "/x402/arc/buy", "/arc/bridge/complete"} {
		t.Run(route, func(t *testing.T) {
			before := rpc.Sends.Load()
			req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{"chain":"arc-testnet","destChain":"base-sepolia","to":"0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC","amount":"0.01","broadcast":true,"txHash":"0x1111111111111111111111111111111111111111111111111111111111111111"}`))
			req.Header.Set("Authorization", "Bearer "+api.TestMakeToken(d.JWTSecret, "owner"))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			var res arc.Result
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusAccepted || res.Status != "pending" || rpc.Sends.Load()-before != 1 {
				t.Fatalf("status=%d result=%+v sends=%d", w.Code, res, rpc.Sends.Load()-before)
			}
			hash := res.TxHash
			if route == "/arc/bridge" || route == "/arc/gateway/deposit" {
				hash = res.ApproveTx
			}
			if hash == "" || hash != rpc.Hash.Load() {
				t.Fatalf("lost identity: %+v", res)
			}
		})
	}
}
