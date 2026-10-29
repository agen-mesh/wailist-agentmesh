package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api"
	"github.com/agentmesh/backend/internal/api/handlers"
)

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
