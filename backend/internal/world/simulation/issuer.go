// Package simulation supplies an explicit loopback issuer for controlled local
// application checks. It never contacts World or proves a real identity.
package simulation

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"time"
)

const (
	issuerURL = "https://sandbox.auth.world.org"
	orbACR    = "https://world.org/oidc/acr/orb"
	Subject   = "local-simulation-subject"
)

type Outcome string

const (
	Success         Outcome = "success"
	Denied          Outcome = "denied"
	Stale           Outcome = "stale"
	WrongAudience   Outcome = "wrong_audience"
	WrongNonce      Outcome = "wrong_nonce"
	WrongIssuer     Outcome = "wrong_issuer"
	Expired         Outcome = "expired"
	MissingAuthTime Outcome = "missing_auth_time"
	FutureAuth      Outcome = "future_auth"
	BadSignature    Outcome = "bad_signature"
	TokenError      Outcome = "token_endpoint_error"
)

func SupportedOutcomes() []Outcome {
	return []Outcome{Success, Denied, Stale, WrongAudience, WrongNonce, WrongIssuer, Expired, MissingAuthTime, FutureAuth, BadSignature, TokenError}
}

var errSimulation = errors.New("world simulation: invalid local configuration or request")

type Registration struct {
	Issuer       string
	ClientID     string
	ClientSecret string `json:"-"`
	RedirectURI  string
}

func (Registration) String() string   { return "world.simulation.Registration{private}" }
func (Registration) GoString() string { return "world.simulation.Registration{private}" }

type grant struct {
	nonce, challenge, subject  string
	authenticatedAt, expiresAt time.Time
	outcome                    Outcome
}

type Observations struct {
	TokenRequests uint64 `json:"token_requests"`
}

type Issuer struct {
	server       *httptest.Server
	registration Registration
	key          *rsa.PrivateKey
	mu           sync.Mutex
	outcome      Outcome
	subject      string
	codes        map[string]grant
	observations Observations
}

// New accepts only the local callback and creates all synthetic credentials.
// Central startup must call it only in explicitly labelled local simulation mode.
func New(redirectURI string) (*Issuer, error) {
	u, err := url.Parse(redirectURI)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.EscapedPath() != "/purchases/world/callback" && u.EscapedPath() != "/api/purchases/world/callback") {
		return nil, errSimulation
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errSimulation
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, errSimulation
	}
	clientID, err := randomString()
	if err != nil {
		return nil, errSimulation
	}
	clientSecret, err := randomString()
	if err != nil {
		return nil, errSimulation
	}
	i := &Issuer{key: key, outcome: Success, subject: Subject, codes: make(map[string]grant), registration: Registration{
		Issuer: issuerURL, ClientID: "local-simulation-" + clientID, ClientSecret: clientSecret, RedirectURI: redirectURI,
	}}
	i.server = httptest.NewServer(http.HandlerFunc(i.serve))
	return i, nil
}

func (i *Issuer) Registration() Registration { return i.registration }
func (i *Issuer) Close()                     { i.server.Close() }

func (i *Issuer) Observations() Observations {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.observations
}

// SetSubject changes only future authorizations; existing codes and observations
// remain intact so changing scenarios cannot erase prior behavior.
func (i *Issuer) SetSubject(subject string) error {
	if len(subject) == 0 || len(subject) > 255 {
		return errSimulation
	}
	for _, c := range subject {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':') {
			return errSimulation
		}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.subject = subject
	return nil
}

func (i *Issuer) SetOutcome(outcome Outcome) error {
	if !slices.Contains(SupportedOutcomes(), outcome) {
		return errSimulation
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.outcome = outcome
	return nil
}

func (i *Issuer) HTTPClient() *http.Client {
	target, _ := url.Parse(i.server.URL)
	base := i.server.Client().Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{Transport: localTransport{target: target, base: base}, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// BrowserURL changes only the discovered sandbox authorization URL into this
// instance's loopback URL. Never expose this mapping in a live environment.
func (i *Issuer) BrowserURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "sandbox.auth.world.org" || u.User != nil || u.Fragment != "" || u.EscapedPath() != "/authorize" {
		return "", errSimulation
	}
	target, _ := url.Parse(i.server.URL)
	u.Scheme = target.Scheme
	u.Host = target.Host
	return u.String(), nil
}

type localTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t localTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "sandbox.auth.world.org" || r.URL.User != nil {
		return nil, errSimulation
	}
	copy := r.Clone(r.Context())
	u := *copy.URL
	u.Scheme = t.target.Scheme
	u.Host = t.target.Host
	copy.URL = &u
	return t.base.RoundTrip(copy)
}

func (i *Issuer) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeJSON(w, map[string]any{
			"issuer": issuerURL, "authorization_endpoint": issuerURL + "/authorize", "token_endpoint": issuerURL + "/token", "jwks_uri": issuerURL + "/jwks",
			"response_types_supported": []string{"code"}, "response_modes_supported": []string{"query"}, "grant_types_supported": []string{"authorization_code"},
			"scopes_supported": []string{"openid"}, "id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic"}, "acr_values_supported": []string{orbACR},
			"claims_supported": []string{"iss", "sub", "aud", "exp", "iat", "nonce", "auth_time", "acr", "amr"},
			"authorization_response_iss_parameter_supported": true, "claims_parameter_supported": true,
		})
	case "/jwks":
		writeJSON(w, map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "local-simulation-key", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(i.key.E)).Bytes())}}})
	case "/authorize":
		i.authorize(w, r)
	case "/token":
		if r.Method == http.MethodPost {
			i.mu.Lock()
			i.observations.TokenRequests++
			i.mu.Unlock()
		}
		i.token(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (i *Issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.Method != http.MethodGet {
		http.Error(w, "invalid_request", 400)
		return
	}
	for _, v := range q {
		if len(v) != 1 {
			http.Error(w, "invalid_request", 400)
			return
		}
	}
	if q.Get("client_id") != i.registration.ClientID || q.Get("redirect_uri") != i.registration.RedirectURI || q.Get("response_type") != "code" ||
		q.Get("response_mode") != "query" || q.Get("scope") != "openid" || q.Get("max_age") != "0" || q.Get("code_challenge_method") != "S256" ||
		len(q.Get("state")) != 43 || len(q.Get("nonce")) != 43 || len(q.Get("code_challenge")) != 43 {
		http.Error(w, "invalid_request", 400)
		return
	}
	callback := url.Values{"state": {q.Get("state")}, "iss": {issuerURL}}
	i.mu.Lock()
	if i.outcome == Denied {
		callback.Set("error", "access_denied")
	} else {
		code, err := randomString()
		if err != nil {
			i.mu.Unlock()
			http.Error(w, "simulation_randomness", 500)
			return
		}
		now := time.Now()
		for old, g := range i.codes {
			if !now.Before(g.expiresAt) {
				delete(i.codes, old)
			}
		}
		if len(i.codes) >= 1024 {
			i.mu.Unlock()
			http.Error(w, "simulation_capacity", 503)
			return
		}
		i.codes[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), subject: i.subject, authenticatedAt: now, expiresAt: now.Add(5 * time.Minute), outcome: i.outcome}
		callback.Set("code", code)
	}
	i.mu.Unlock()
	http.Redirect(w, r, i.registration.RedirectURI+"?"+callback.Encode(), http.StatusFound)
}

func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.ParseForm() != nil {
		http.Error(w, "invalid_request", 400)
		return
	}
	p := r.PostForm
	for _, v := range p {
		if len(v) != 1 {
			http.Error(w, "invalid_request", 400)
			return
		}
	}
	id, secret, ok := r.BasicAuth()
	id, ie := url.QueryUnescape(id)
	secret, se := url.QueryUnescape(secret)
	if !ok || ie != nil || se != nil || id != i.registration.ClientID || secret != i.registration.ClientSecret || p.Get("client_id") != id || p.Has("client_secret") || p.Has("client_assertion") {
		http.Error(w, "invalid_client", 401)
		return
	}
	i.mu.Lock()
	g, found := i.codes[p.Get("code")]
	challenge := sha256.Sum256([]byte(p.Get("code_verifier")))
	valid := found && time.Now().Before(g.expiresAt) && p.Get("grant_type") == "authorization_code" && p.Get("redirect_uri") == i.registration.RedirectURI && base64.RawURLEncoding.EncodeToString(challenge[:]) == g.challenge
	if valid {
		delete(i.codes, p.Get("code"))
	}
	i.mu.Unlock()
	if !valid {
		http.Error(w, "invalid_grant", 400)
		return
	}
	if g.outcome == TokenError {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{"iss": issuerURL, "sub": g.subject, "aud": i.registration.ClientID, "exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(),
		"nonce": g.nonce, "auth_time": g.authenticatedAt.Unix(), "acr": orbACR, "amr": []string{"pop"}}
	switch g.outcome {
	case Stale:
		claims["auth_time"] = now.Add(-time.Hour).Unix()
	case WrongAudience:
		claims["aud"] = "local-simulation-other-client"
	case WrongNonce:
		claims["nonce"] = "local-simulation-wrong-nonce"
	case WrongIssuer:
		claims["iss"] = "https://auth.world.org"
	case Expired:
		claims["exp"] = now.Add(-time.Minute).Unix()
	case MissingAuthTime:
		delete(claims, "auth_time")
	case FutureAuth:
		claims["auth_time"] = now.Add(time.Hour).Unix()
	}
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "local-simulation-key", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		http.Error(w, "simulation_signing", 500)
		return
	}
	if g.outcome == BadSignature {
		signature[0] ^= 0x80
	}
	writeJSON(w, map[string]any{"id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), "access_token": "local-simulation-only", "token_type": "Bearer", "expires_in": 300})
}

func randomString() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
func writeJSON(w http.ResponseWriter, value any) { _ = json.NewEncoder(w).Encode(value) }
