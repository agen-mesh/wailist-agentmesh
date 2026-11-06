package world

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// This issuer independently owns authorization codes, client authentication,
// PKCE and signing. It never calls the implementation's verification helpers.
type issuerFixture struct {
	t         *testing.T
	server    *httptest.Server
	http      *http.Client
	cfg       Config
	now       time.Time
	key       *rsa.PrivateKey
	kid       string
	keys      []map[string]any
	mu        sync.Mutex
	codes     map[string]fixtureGrant
	jtis      map[string]bool
	exchanges int
	jwksReads int
	denied    bool
	status    int
	started   chan struct{}
	release   chan struct{}
	metadata  func(map[string]any)
	payload   func(map[string]any)
	header    func(map[string]any)
	keySet    func(map[string]any)
}

type fixtureGrant struct {
	nonce, challenge, redirect string
	used                       bool
	expiresAt                  time.Time
}

var fixtureKeys struct {
	sync.Once
	issuer, other, client *rsa.PrivateKey
}

func loadFixtureKeys(t *testing.T) {
	t.Helper()
	fixtureKeys.Do(func() {
		var err error
		fixtureKeys.issuer, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		fixtureKeys.other, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		fixtureKeys.client, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
	})
}

type localIssuerTransport struct {
	host   string
	target *url.URL
	base   http.RoundTripper
}

func (t localIssuerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || req.URL.Host != t.host {
		return nil, errors.New("fixture rejects external network")
	}
	copy := req.Clone(req.Context())
	u := *copy.URL
	u.Scheme, u.Host = t.target.Scheme, t.target.Host
	copy.URL = &u
	return t.base.RoundTrip(copy)
}

func newIssuer(t *testing.T, environment Environment) *issuerFixture {
	t.Helper()
	loadFixtureKeys(t)
	issuer := SandboxIssuer
	if environment == Production {
		issuer = ProductionIssuer
	}
	f := &issuerFixture{
		t: t, now: time.Now().UTC().Truncate(time.Second), key: fixtureKeys.issuer, kid: "fixture-issuer-key",
		codes: make(map[string]fixtureGrant), jtis: make(map[string]bool),
		cfg: Config{Enabled: true, Environment: environment, Issuer: issuer,
			ClientID: "fixture client:+", ClientSecret: "fixture secret: +&", AuthMethod: ClientSecretBasic,
			RedirectURI: "https://app.example/purchases/world/callback", RequiredACR: OrbACR},
	}
	f.keys = []map[string]any{fixtureJWK(f.key, f.kid)}
	f.server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	target, _ := url.Parse(f.server.URL)
	u, _ := url.Parse(issuer)
	f.http = &http.Client{Transport: localIssuerTransport{u.Host, target, f.server.Client().Transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return f
}

func fixtureJWK(key *rsa.PrivateKey, kid string) map[string]any {
	return map[string]any{"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}
}

func (f *issuerFixture) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		acrs := []string{OrbACR}
		if f.cfg.Environment == Sandbox {
			acrs = append(acrs, OrbFaceACR)
		}
		d := map[string]any{
			"issuer": f.cfg.Issuer, "authorization_endpoint": f.cfg.Issuer + "/authorize",
			"token_endpoint": f.cfg.Issuer + "/token", "jwks_uri": f.cfg.Issuer + "/jwks",
			"response_types_supported": []string{"code"}, "response_modes_supported": []string{"query"},
			"grant_types_supported": []string{"authorization_code"}, "scopes_supported": []string{"openid"},
			"id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"},
			"token_endpoint_auth_methods_supported":            []string{"client_secret_basic", "client_secret_post", "private_key_jwt"},
			"token_endpoint_auth_signing_alg_values_supported": []string{"RS256"},
			"acr_values_supported":                             acrs, "claims_parameter_supported": true,
			"authorization_response_iss_parameter_supported": true,
			"claims_supported": []string{"iss", "sub", "aud", "exp", "iat", "nonce", "auth_time", "acr", "amr"},
		}
		if f.metadata != nil {
			f.metadata(d)
		}
		_ = json.NewEncoder(w).Encode(d)
	case "/jwks":
		f.mu.Lock()
		f.jwksReads++
		f.mu.Unlock()
		set := map[string]any{"keys": f.keys}
		if f.keySet != nil {
			f.keySet(set)
		}
		_ = json.NewEncoder(w).Encode(set)
	case "/authorize":
		f.authorize(w, r)
	case "/token":
		f.redeem(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *issuerFixture) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if r.Method != http.MethodGet || q.Get("client_id") != f.cfg.ClientID ||
		q.Get("redirect_uri") != f.cfg.RedirectURI || q.Get("response_type") != "code" ||
		q.Get("scope") != "openid" || q.Get("response_mode") != "query" || q.Get("max_age") != "0" ||
		q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 ||
		len(q.Get("state")) != 43 || len(q.Get("nonce")) != 43 {
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	callback := url.Values{"state": {q.Get("state")}, "iss": {f.cfg.Issuer}}
	if f.denied {
		callback.Set("error", "access_denied")
	} else {
		var raw [24]byte
		_, _ = rand.Read(raw[:])
		code := base64.RawURLEncoding.EncodeToString(raw[:])
		f.mu.Lock()
		f.codes[code] = fixtureGrant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), expiresAt: f.now.Add(5 * time.Minute)}
		f.mu.Unlock()
		callback.Set("code", code)
	}
	http.Redirect(w, r, f.cfg.RedirectURI+"?"+callback.Encode(), http.StatusFound)
}

func (f *issuerFixture) redeem(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.exchanges++
	f.mu.Unlock()
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.ParseForm() != nil {
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	for _, values := range r.PostForm {
		if len(values) != 1 {
			http.Error(w, "invalid_request", http.StatusBadRequest)
			return
		}
	}
	if !f.clientAuthenticated(r) {
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	g, exists := f.codes[r.PostForm.Get("code")]
	challenge := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	valid := exists && !g.used && f.now.Before(g.expiresAt) && r.PostForm.Get("grant_type") == "authorization_code" &&
		r.PostForm.Get("redirect_uri") == g.redirect && base64.RawURLEncoding.EncodeToString(challenge[:]) == g.challenge
	if valid {
		g.used = true
		f.codes[r.PostForm.Get("code")] = g
	}
	f.mu.Unlock()
	if !valid {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
		return
	}
	if f.started != nil {
		close(f.started)
		<-f.release
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	claims := map[string]any{"iss": f.cfg.Issuer, "sub": "fixture-subject", "aud": f.cfg.ClientID,
		"iat": f.now.Unix(), "exp": f.now.Add(5 * time.Minute).Unix(), "auth_time": f.now.Unix(),
		"nonce": g.nonce, "acr": OrbACR, "amr": []string{"pop", "iris"}}
	if f.payload != nil {
		f.payload(claims)
	}
	header := map[string]any{"alg": "RS256", "kid": f.kid, "typ": "JWT"}
	if f.header != nil {
		f.header(header)
	}
	raw := signFixtureToken(f.t, f.key, header, claims)
	_ = json.NewEncoder(w).Encode(map[string]any{"id_token": raw, "access_token": "fixture-opaque-token", "token_type": "Bearer", "expires_in": 300})
}

func signFixtureToken(t *testing.T, key *rsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Error("fixture signing error")
		return ""
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (f *issuerFixture) clientAuthenticated(r *http.Request) bool {
	p := r.PostForm
	if p.Get("client_id") != f.cfg.ClientID {
		return false
	}
	switch f.cfg.AuthMethod {
	case ClientSecretBasic:
		id, secret, ok := r.BasicAuth()
		id, ie := url.QueryUnescape(id)
		secret, se := url.QueryUnescape(secret)
		return ok && ie == nil && se == nil && id == f.cfg.ClientID && secret == f.cfg.ClientSecret && !p.Has("client_secret") && !p.Has("client_assertion")
	case ClientSecretPost:
		return r.Header.Get("Authorization") == "" && p.Get("client_secret") == f.cfg.ClientSecret && !p.Has("client_assertion")
	case PrivateKeyJWT:
		if r.Header.Get("Authorization") != "" || p.Has("client_secret") || p.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
			return false
		}
		parts := strings.Split(p.Get("client_assertion"), ".")
		if len(parts) != 3 {
			return false
		}
		header, he := base64.RawURLEncoding.DecodeString(parts[0])
		body, be := base64.RawURLEncoding.DecodeString(parts[1])
		signature, se := base64.RawURLEncoding.DecodeString(parts[2])
		var h, claims map[string]any
		if he != nil || be != nil || se != nil || json.Unmarshal(header, &h) != nil || json.Unmarshal(body, &claims) != nil {
			return false
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if h["alg"] != "RS256" || h["kid"] != f.cfg.PrivateKeyID || rsa.VerifyPKCS1v15(&fixtureKeys.client.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			return false
		}
		aud, ok := claims["aud"].([]any)
		iat, iok := claims["iat"].(float64)
		exp, eok := claims["exp"].(float64)
		jti, jok := claims["jti"].(string)
		if !ok || len(aud) != 1 || aud[0] != f.cfg.Issuer+"/token" || claims["iss"] != f.cfg.ClientID || claims["sub"] != f.cfg.ClientID ||
			!iok || !eok || !jok || jti == "" || exp <= float64(f.now.Unix()) || exp-iat > 3600 {
			return false
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.jtis[jti] {
			return false
		}
		f.jtis[jti] = true
		return true
	}
	return false
}

func (f *issuerFixture) callback(t *testing.T, authorization Authorization) url.Values {
	t.Helper()
	resp, err := f.http.Get(authorization.AuthorizationURL)
	if err != nil {
		t.Fatal("fixture authorization request:", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatal("fixture authorization status:", resp.StatusCode)
	}
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func (f *issuerFixture) client(t *testing.T, store Store) *Client {
	t.Helper()
	c, err := New(context.Background(), f.cfg, store, f.http, WithClock(func() time.Time { return f.now }))
	if err != nil {
		t.Fatal("new fixture client:", err)
	}
	return c
}

func fixtureBinding() Binding {
	return Binding{AccountID: "account-1", OrderID: "order-1", IntentHash: strings.Repeat("a", 64), ExpectedSubject: "fixture-subject"}
}

func fixturePrivatePEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(fixtureKeys.client)}))
}

type storedFixtureAttempt struct {
	attempt  Attempt
	status   string
	evidence Evidence
}
type fixtureStore struct {
	mu                                 sync.Mutex
	rows                               map[string]*storedFixtureAttempt
	createErr, consumeErr, completeErr error
	corrupt                            func(*Attempt)
	completed                          int
}

func newFixtureStore() *fixtureStore {
	return &fixtureStore{rows: make(map[string]*storedFixtureAttempt)}
}

func (s *fixtureStore) Create(_ context.Context, a Attempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return s.createErr
	}
	if s.rows[a.StateHash] != nil {
		return errors.New("duplicate state")
	}
	for _, row := range s.rows {
		if row.attempt.ID == a.ID {
			return errors.New("duplicate ID")
		}
	}
	s.rows[a.StateHash] = &storedFixtureAttempt{attempt: a, status: "pending"}
	return nil
}

func (s *fixtureStore) Consume(_ context.Context, state string, binding Binding, now time.Time) (Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumeErr != nil {
		return Attempt{}, s.consumeErr
	}
	r := s.rows[state]
	if r == nil || r.status != "pending" || r.attempt.Binding != binding || !now.Before(r.attempt.ExpiresAt) {
		return Attempt{}, ErrAttemptUnavailable
	}
	r.status = "consumed"
	a := r.attempt
	if s.corrupt != nil {
		s.corrupt(&a)
	}
	return a, nil
}

func (s *fixtureStore) Complete(_ context.Context, id string, e Evidence, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completeErr != nil {
		return s.completeErr
	}
	for _, r := range s.rows {
		if r.attempt.ID == id && r.status == "consumed" && r.attempt.Binding == e.Binding && e.AttemptID == id && now.Before(r.attempt.ExpiresAt) && now.Before(e.ValidUntil) {
			r.status = "validated"
			r.evidence = e
			s.completed++
			return nil
		}
	}
	return ErrAttemptUnavailable
}

func (s *fixtureStore) Cancel(_ context.Context, id string, b Binding, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.attempt.ID == id && r.attempt.Binding == b && (r.status == "pending" || r.status == "consumed") {
			r.status = "cancelled"
			return nil
		}
	}
	return ErrAttemptUnavailable
}

func beginFixture(t *testing.T, f *issuerFixture, c *Client, b Binding) (Authorization, url.Values) {
	t.Helper()
	a, err := c.Begin(context.Background(), b)
	if err != nil {
		t.Fatal("begin fixture:", err)
	}
	return a, f.callback(t, a)
}

func assertNoEvidence(t *testing.T, e Evidence, err error) {
	t.Helper()
	if err == nil || e.AttemptID != "" || e.Subject != "" || !e.ValidUntil.IsZero() {
		t.Fatal("rejected callback yielded evidence")
	}
}
