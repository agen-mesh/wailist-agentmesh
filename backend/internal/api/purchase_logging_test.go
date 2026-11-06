package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/arc"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func TestPurchaseCallbackQueryIsHiddenBeforeLogEntryCreation(t *testing.T) {
	var output bytes.Buffer
	previous := chimw.DefaultLogger
	chimw.DefaultLogger = chimw.RequestLogger(&chimw.DefaultLogFormatter{Logger: log.New(&output, "", 0), NoColor: true})
	t.Cleanup(func() { chimw.DefaultLogger = previous })
	seen := false
	h := purchaseSafeLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query().Get("code") == "code-sentinel" && r.URL.Query().Get("state") == "state-sentinel"
		if chimw.GetLogEntry(r) == nil {
			t.Error("request log context lost")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/purchases/world/callback?code=code-sentinel&state=state-sentinel&token=token-sentinel", nil))
	if !seen || !strings.Contains(output.String(), "/purchases/world/callback") {
		t.Fatal("callback query or route logging lost")
	}
	for _, secret := range []string{"code-sentinel", "state-sentinel", "token-sentinel"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("callback secret reached the logger")
		}
	}
	output.Reset()
	router := NewRouter(&handlers.Deps{ArcConfig: &arc.Config{}, JWTSecret: "scratch-only"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/purchases/world/callback?code=code-sentinel&state=state-sentinel", nil))
	if rr.Code != http.StatusUnauthorized || strings.Contains(rr.Body.String()+output.String(), "sentinel") {
		t.Fatal("actual router exposed callback details")
	}
}

func TestPurchaseRawRoutesRequireAuthenticatedAccount(t *testing.T) {
	router := NewRouter(&handlers.Deps{ArcConfig: &arc.Config{}, JWTSecret: "scratch-only"})
	for _, path := range []string{"/purchases", "/purchases/wallet/link", "/purchases/order/decision", "/purchases/order/authorization", "/purchases/order/submit", "/purchases/order/reconcile"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("POST", path, strings.NewReader(`{"operator":true,"role":"operator","worldVerified":true}`)))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s returned %d", path, rr.Code)
		}
	}
}
