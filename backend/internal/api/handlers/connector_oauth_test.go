package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/models"
)

// testStore returns a *db.Store backed by TEST_DATABASE_URL, skipping the
// test when it isn't set — same convention as testDeps in workflows_test.go,
// just returning the bare store since these tests build Deps by hand.
func testStore(t *testing.T) *db.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

// setupConnectorTestFixtures creates a user id, a workflow owned by it, and a
// single action node on that workflow (template "fakeprovider"), returning
// (userID, workflowID, nodeID) for the OAuth-linking tests to target.
func setupConnectorTestFixtures(t *testing.T, store *db.Store) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	userID := "connector-oauth-test-user"

	wf, err := store.CreateWorkflow(ctx, "test", userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DeleteWorkflow(context.Background(), wf.ID) })

	_, err = store.UpdateWorkflow(ctx, wf.ID, wf.Name, models.WorkflowGraph{
		Nodes: []models.WorkflowNode{{ID: "node1", Type: models.NodeTypeAction, Template: "fakeprovider"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	return userID, wf.ID, "node1"
}

// mustParseLocationState parses the Location header's state query param.
func mustParseLocationState(t *testing.T, location string) string {
	t.Helper()
	loc, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	return loc.Query().Get("state")
}

// fakeProviderServer stands in for a third-party OAuth provider: it serves
// both the authorize redirect target (never actually hit by Go code — the
// browser would go there) and the token endpoint the callback handler POSTs
// to, so the test only needs to fake the token endpoint.
func fakeProviderServer(t *testing.T, wantCode string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.FormValue("code") != wantCode {
			t.Fatalf("code = %q, want %q", r.FormValue("code"), wantCode)
		}
		if r.FormValue("client_id") != "test-client-id" || r.FormValue("client_secret") != "test-client-secret" {
			t.Fatalf("client credentials missing from token exchange: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","expires_in":3600}`))
	}))
}

func TestConnectorOAuthStartRedirectsToProviderWithSignedState(t *testing.T) {
	store := testStore(t)
	d := &handlers.Deps{Store: store, JWTSecret: "test-jwt-secret-not-for-production-use-only-32b", BaseURL: "https://example.test", EncryptionKey: "00000000000000000000000000000000"}
	handlers.SetConnectorProviderForTest("fakeprovider", handlers.ConnectorOAuthConfig{
		AuthURL: "https://fakeprovider.test/authorize", TokenURL: "https://fakeprovider.test/token",
		Scope: "read write", ClientIDEnvVal: "test-client-id", ClientSecretEnvVal: "test-client-secret",
	})
	defer handlers.ClearConnectorProviderForTest("fakeprovider")

	userID, workflowID, nodeID := setupConnectorTestFixtures(t, store)

	req := httptest.NewRequest(http.MethodGet, "/connectors/oauth/fakeprovider/start?workflowId="+workflowID+"&nodeId="+nodeID, nil)
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, userID))
	req = withURLParam(req, "provider", "fakeprovider")
	rec := httptest.NewRecorder()

	d.ConnectorOAuthStart(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Scheme+"://"+loc.Host+loc.Path != "https://fakeprovider.test/authorize" {
		t.Fatalf("redirected to %q, want fakeprovider authorize URL", rec.Header().Get("Location"))
	}
	if loc.Query().Get("state") == "" {
		t.Fatal("missing state param")
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected a state cookie to be set")
	}
}

func TestConnectorOAuthCallbackWritesEncryptedTokenIntoNodeSecrets(t *testing.T) {
	provider := fakeProviderServer(t, "the-auth-code")
	defer provider.Close()

	store := testStore(t)
	d := &handlers.Deps{Store: store, JWTSecret: "test-jwt-secret-not-for-production-use-only-32b", BaseURL: "https://example.test", EncryptionKey: "00000000000000000000000000000000"}
	handlers.SetConnectorProviderForTest("fakeprovider", handlers.ConnectorOAuthConfig{
		AuthURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token",
		Scope: "read write", ClientIDEnvVal: "test-client-id", ClientSecretEnvVal: "test-client-secret",
	})
	defer handlers.ClearConnectorProviderForTest("fakeprovider")

	userID, workflowID, nodeID := setupConnectorTestFixtures(t, store)

	// Drive Start first to get a real signed state + matching cookie, exactly
	// as the browser round-trip would.
	startReq := httptest.NewRequest(http.MethodGet, "/connectors/oauth/fakeprovider/start?workflowId="+workflowID+"&nodeId="+nodeID, nil)
	startReq = startReq.WithContext(context.WithValue(startReq.Context(), handlers.CtxUserID, userID))
	startReq = withURLParam(startReq, "provider", "fakeprovider")
	startRec := httptest.NewRecorder()
	d.ConnectorOAuthStart(startRec, startReq)
	state := mustParseLocationState(t, startRec.Header().Get("Location"))
	stateCookie := startRec.Result().Cookies()[0]

	cbReq := httptest.NewRequest(http.MethodGet, "/connectors/oauth/fakeprovider/callback?code=the-auth-code&state="+url.QueryEscape(state), nil)
	cbReq = cbReq.WithContext(context.WithValue(cbReq.Context(), handlers.CtxUserID, userID))
	cbReq = withURLParam(cbReq, "provider", "fakeprovider")
	cbReq.AddCookie(stateCookie)
	cbRec := httptest.NewRecorder()

	d.ConnectorOAuthCallback(cbRec, cbReq)

	if cbRec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", cbRec.Code, http.StatusFound, cbRec.Body.String())
	}

	wf, err := store.GetWorkflow(context.Background(), workflowID)
	if err != nil {
		t.Fatal(err)
	}
	var node models.WorkflowNode
	for _, n := range wf.Nodes {
		if n.ID == nodeID {
			node = n
		}
	}
	enc := node.Secrets["fakeproviderOAuthAccessToken"]
	if enc == "" || !strings.HasPrefix(enc, "enc:") {
		t.Fatalf("fakeproviderOAuthAccessToken = %q, want encrypted (enc: prefix)", enc)
	}
	if node.Config["fakeproviderOAuthExpiresAt"] == "" {
		t.Fatal("expected fakeproviderOAuthExpiresAt to be set in Config")
	}
}

func TestConnectorOAuthCallbackRejectsMismatchedState(t *testing.T) {
	store := testStore(t)
	d := &handlers.Deps{Store: store, JWTSecret: "test-jwt-secret-not-for-production-use-only-32b", BaseURL: "https://example.test", EncryptionKey: "00000000000000000000000000000000"}
	handlers.SetConnectorProviderForTest("fakeprovider", handlers.ConnectorOAuthConfig{
		AuthURL: "https://fakeprovider.test/authorize", TokenURL: "https://fakeprovider.test/token",
		ClientIDEnvVal: "test-client-id", ClientSecretEnvVal: "test-client-secret",
	})
	defer handlers.ClearConnectorProviderForTest("fakeprovider")

	userID, workflowID, nodeID := setupConnectorTestFixtures(t, store)
	startReq := httptest.NewRequest(http.MethodGet, "/connectors/oauth/fakeprovider/start?workflowId="+workflowID+"&nodeId="+nodeID, nil)
	startReq = startReq.WithContext(context.WithValue(startReq.Context(), handlers.CtxUserID, userID))
	startReq = withURLParam(startReq, "provider", "fakeprovider")
	startRec := httptest.NewRecorder()
	d.ConnectorOAuthStart(startRec, startReq)
	stateCookie := startRec.Result().Cookies()[0]

	cbReq := httptest.NewRequest(http.MethodGet, "/connectors/oauth/fakeprovider/callback?code=whatever&state=tampered-state-value", nil)
	cbReq = cbReq.WithContext(context.WithValue(cbReq.Context(), handlers.CtxUserID, userID))
	cbReq = withURLParam(cbReq, "provider", "fakeprovider")
	cbReq.AddCookie(stateCookie)
	cbRec := httptest.NewRecorder()

	d.ConnectorOAuthCallback(cbRec, cbReq)

	loc := cbRec.Header().Get("Location")
	if !strings.Contains(loc, "connectError=") {
		t.Fatalf("expected redirect to carry a connectError, got %q", loc)
	}
}
