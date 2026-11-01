package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/world"
	worldsim "github.com/agentmesh/backend/internal/world/simulation"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const worldStateCookie = "purchase_world_state"

type identityBrowserFixture struct {
	store     *db.Store
	server    *httptest.Server
	issuer    *worldsim.Issuer
	deps      *handlers.Deps
	account   string
	other     string
	order     purchase.Order
	browser   *http.Client
	publicURL string
	prefix    string
}

func newIdentityBrowserFixture(t *testing.T, proxyMode ...bool) *identityBrowserFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatal("open scratch database")
	}
	t.Cleanup(store.Close)
	f := &identityBrowserFixture{store: store}
	for _, target := range []*string{&f.account, &f.other} {
		u, err := store.CreateUser(t.Context(), uuid.NewString()+"@browser.invalid", "unused-test-hash")
		if err != nil {
			t.Fatal("create scratch account")
		}
		*target = u.ID
	}
	mux := http.NewServeMux()
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	f.publicURL = f.server.URL
	if len(proxyMode) != 0 && proxyMode[0] {
		target, _ := url.Parse(f.server.URL)
		proxy := httputil.NewSingleHostReverseProxy(target)
		frontend := httptest.NewServer(http.StripPrefix("/api", proxy))
		t.Cleanup(frontend.Close)
		f.publicURL = strings.Replace(frontend.URL, "127.0.0.1", "localhost", 1)
		f.prefix = "/api"
	}
	f.issuer, err = worldsim.New(f.publicURL + f.prefix + "/purchases/world/callback")
	if err != nil {
		t.Fatal("create controlled issuer")
	}
	t.Cleanup(f.issuer.Close)
	if err = f.issuer.SetSubject(uuid.NewString()); err != nil {
		t.Fatal("set isolated subject")
	}
	registration := f.issuer.Registration()
	identities := store.PurchaseIdentities("01234567890123456789012345678901")
	client, err := world.New(t.Context(), world.Config{Enabled: true, Environment: world.Sandbox, Issuer: registration.Issuer, ClientID: registration.ClientID, ClientSecret: registration.ClientSecret, RedirectURI: registration.RedirectURI}, identities, f.issuer.HTTPClient())
	if err != nil {
		t.Fatal("construct real identity validator")
	}
	identity := &purchase.WorldIdentity{Client: client, Store: identities, Issuer: registration.Issuer, AuthorizationURL: f.issuer.BrowserURL}
	service := &purchase.Service{Enabled: true, Store: store.Purchases(), Identity: identity, Catalog: purchase.ArkivCatalog{}, Merchant: &purchase.MerchantClient{}, Rail: &purchase.ArcRail{}, Domain: f.server.URL, Seal: func(s string) (string, error) { return s, nil }, Open: func(s string) (string, error) { return s, nil }}
	f.deps = &handlers.Deps{Store: store, JWTSecret: "browser-binding-scratch", BaseURL: f.server.URL, FrontendURL: f.publicURL, ArcConfig: &arc.Config{}, Purchases: &handlers.PurchaseRuntime{Service: service, Identity: identity}}
	mux.Handle("/", NewRouter(f.deps))
	f.order = purchase.Order{ID: uuid.NewString(), AccountID: f.account, ClientRequestID: uuid.NewString(), State: purchase.AwaitingIdentity, MerchantAccepted: true, CreatedAt: time.Now().UTC()}
	f.order.Intent = purchase.Intent{Version: purchase.Protocol, OrderID: f.order.ID, AccountID: f.account, ChainID: purchase.ChainID, Asset: purchase.Asset, Payer: "0x1111111111111111111111111111111111111111", Payee: "0x2222222222222222222222222222222222222222", AmountMicros: "100", MaxMicros: "100", OrderSalt: purchase.Digest([]byte(f.order.ID)), ValidBefore: strconv.FormatInt(time.Now().Add(4*time.Minute).Unix(), 10)}
	raw, err := json.Marshal(f.order.Intent)
	if err != nil {
		t.Fatal("encode fixture intent")
	}
	f.order.IntentHash = purchase.Digest(raw)
	if _, created, err := store.Purchases().Create(t.Context(), f.order); err != nil || !created {
		t.Fatal("persist fixture order")
	}
	f.browser = f.newBrowser(t, f.account)
	return f
}

func (f *identityBrowserFixture) newBrowser(t *testing.T, account string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal("create isolated cookie jar")
	}
	if account != "" {
		u, _ := url.Parse(f.publicURL)
		jar.SetCookies(u, []*http.Cookie{{Name: "agentmesh_token", Value: TestMakeToken(f.deps.JWTSecret, account), Path: "/"}})
	}
	return &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func identityResponse(t *testing.T, client *http.Client, req *http.Request) *http.Response {
	t.Helper()
	response, err := client.Do(req)
	if err != nil {
		t.Fatal("controlled identity request failed")
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func (f *identityBrowserFixture) start(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	req, _ := http.NewRequest("POST", f.publicURL+f.prefix+"/purchases/"+f.order.ID+"/identity", nil)
	response := identityResponse(t, f.browser, req)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("identity start status=%d", response.StatusCode)
	}
	var start purchase.IdentityStart
	if err := json.NewDecoder(response.Body).Decode(&start); err != nil {
		t.Fatal("decode identity start")
	}
	var cookie *http.Cookie
	for _, c := range response.Cookies() {
		if c.Name == worldStateCookie {
			cookie = c
		}
	}
	return start.AuthorizationURL, cookie
}

func (f *identityBrowserFixture) authorize(t *testing.T, authorizationURL string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", authorizationURL, nil)
	response := identityResponse(t, client, req)
	if response.StatusCode != http.StatusFound {
		t.Fatalf("controlled authorization status=%d", response.StatusCode)
	}
	return response.Header.Get("Location")
}

func TestPurchaseWorldCallbackBindsInitiatingBrowserAndAccount(t *testing.T) {
	for _, scenario := range []string{"no_session", "expired_session", "no_state_cookie", "wrong_state_cookie", "different_account", "duplicate_cookie", "duplicate_state"} {
		t.Run(scenario, func(t *testing.T) {
			f := newIdentityBrowserFixture(t)
			authorizationURL, _ := f.start(t)
			callback := f.authorize(t, authorizationURL)
			u, _ := url.Parse(callback)
			state := u.Query().Get("state")
			account := f.account
			want := http.StatusBadRequest
			if scenario == "no_session" || scenario == "expired_session" {
				account, want = "", http.StatusUnauthorized
			}
			if scenario == "different_account" {
				account = f.other
			}
			otherBrowser := f.newBrowser(t, account)
			if scenario == "expired_session" {
				claims := jwtClaims{UserID: f.account, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))}}
				token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(f.deps.JWTSecret))
				if err != nil {
					t.Fatal("sign expired scratch session")
				}
				otherBrowser.Jar.SetCookies(u, []*http.Cookie{{Name: "agentmesh_token", Value: token, Path: "/"}})
			}
			req, _ := http.NewRequest("GET", callback, nil)
			switch scenario {
			case "wrong_state_cookie":
				req.AddCookie(&http.Cookie{Name: worldStateCookie, Value: "unrelated-browser-state"})
			case "different_account", "no_session", "expired_session":
				req.AddCookie(&http.Cookie{Name: worldStateCookie, Value: state})
			case "duplicate_cookie":
				req.AddCookie(&http.Cookie{Name: worldStateCookie, Value: state})
				req.AddCookie(&http.Cookie{Name: worldStateCookie, Value: state})
			case "duplicate_state":
				req.AddCookie(&http.Cookie{Name: worldStateCookie, Value: state})
				query := req.URL.Query()
				query.Add("state", state)
				req.URL.RawQuery = query.Encode()
			}
			response := identityResponse(t, otherBrowser, req)
			if response.StatusCode != want {
				t.Errorf("cross-session callback status=%d want=%d", response.StatusCode, want)
			}
			if calls := f.issuer.Observations().TokenRequests; calls != 0 {
				t.Errorf("rejected callback reached token exchange: %d", calls)
			}
			order, err := f.store.Purchases().Get(t.Context(), f.account, f.order.ID)
			if err != nil || order.State != purchase.AwaitingIdentity || order.IdentityVerifiedAt != nil {
				t.Error("cross-session callback enrolled identity or advanced order")
			}
			binding, err := f.deps.Purchases.Identity.Store.Binding(t.Context(), f.account, f.order.ID, f.order.IntentHash, world.SandboxIssuer)
			if err != nil || !binding.AllowEnrollment {
				t.Error("cross-session callback claimed the subject")
			}
			if t.Failed() {
				return
			}
			f.complete(t, callback)
		})
	}
}

func TestPurchaseWorldCallbackThroughFrontendProxy(t *testing.T) {
	f := newIdentityBrowserFixture(t, true)
	authorizationURL, _ := f.start(t)
	callback := f.authorize(t, authorizationURL)
	u, _ := url.Parse(callback)
	if u.Scheme+"://"+u.Host != f.publicURL || u.Path != "/api/purchases/world/callback" {
		t.Fatal("issuer did not preserve the exact registered proxy callback")
	}
	backendURL, _ := url.Parse(f.server.URL)
	if len(f.browser.Jar.Cookies(backendURL)) != 0 {
		t.Fatal("browser cookies crossed to the backend host")
	}
	direct, _ := http.NewRequest("GET", f.server.URL+"/purchases/world/callback?"+u.RawQuery, nil)
	rejected := identityResponse(t, f.browser, direct)
	if rejected.StatusCode != http.StatusUnauthorized || f.issuer.Observations().TokenRequests != 0 {
		t.Fatal("backend origin accepted a callback without its browser session")
	}
	f.complete(t, callback)
}

func (f *identityBrowserFixture) complete(t *testing.T, callback string) {
	t.Helper()
	req, _ := http.NewRequest("GET", callback, nil)
	response := identityResponse(t, f.browser, req)
	if response.StatusCode != http.StatusSeeOther || f.issuer.Observations().TokenRequests != 1 {
		t.Fatalf("legitimate callback status=%d, exchanges=%d", response.StatusCode, f.issuer.Observations().TokenRequests)
	}
	order, err := f.store.Purchases().Get(t.Context(), f.account, f.order.ID)
	if err != nil || order.State != purchase.AwaitingApproval || order.IdentityVerifiedAt == nil {
		t.Fatal("legitimate identity was not durably recorded")
	}
	cleared := false
	for _, cookie := range response.Cookies() {
		if cookie.Name == worldStateCookie && cookie.MaxAge < 0 && cookie.Value == "" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("accepted callback did not clear browser state")
	}
	for _, restoreCookie := range []bool{false, true} {
		req, _ = http.NewRequest("GET", callback, nil)
		if restoreCookie {
			req.AddCookie(&http.Cookie{Name: worldStateCookie, Value: req.URL.Query().Get("state")})
		}
		replayed := identityResponse(t, f.browser, req)
		io.Copy(io.Discard, replayed.Body)
		if replayed.StatusCode != http.StatusBadRequest || f.issuer.Observations().TokenRequests != 1 {
			t.Fatal("callback replay reached token exchange")
		}
	}
}

func TestPurchaseWorldStateCookiePolicy(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(strconv.FormatBool(secure), func(t *testing.T) {
			f := newIdentityBrowserFixture(t)
			if secure {
				f.deps.BaseURL = "https://backend.example"
			}
			authorizationURL, cookie := f.start(t)
			u, _ := url.Parse(authorizationURL)
			if cookie == nil || cookie.Value != u.Query().Get("state") || cookie.Path != "/" || cookie.Domain != "" || !cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 || cookie.MaxAge > 300 || !cookie.Expires.After(time.Now()) {
				t.Fatal("identity state cookie lacks bounded host-only browser protection")
			}
		})
	}
}
