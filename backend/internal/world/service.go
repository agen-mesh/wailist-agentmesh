package world

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	config     Config
	store      Store
	http       *http.Client
	now        func() time.Time
	privateKey *rsa.PrivateKey
	discovery  discovery
	keys       keyCache
}

type Option func(*Client)

// WithClock is for controlled simulations. Production must use a trusted clock.
func WithClock(now func() time.Time) Option {
	return func(c *Client) { c.now = now }
}

func New(ctx context.Context, cfg Config, store Store, httpClient *http.Client, options ...Option) (*Client, error) {
	cfg, key, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrDisabled
	}
	if store == nil {
		return nil, ErrConfig
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if httpClient != nil {
		*client = *httpClient
		if client.Timeout <= 0 || client.Timeout > 10*time.Second {
			client.Timeout = 10 * time.Second
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := &Client{config: cfg, store: store, http: client, now: time.Now, privateKey: key}
	for _, option := range options {
		if option == nil {
			return nil, ErrConfig
		}
		option(c)
	}
	if c.now == nil {
		return nil, ErrConfig
	}
	if err := c.discover(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) Begin(ctx context.Context, binding Binding) (Authorization, error) {
	if !validBinding(binding) {
		return Authorization{}, ErrBinding
	}
	values := make([]string, 4)
	for i := range values {
		value, err := randomValue()
		if err != nil {
			return Authorization{}, ErrProvider
		}
		values[i] = value
	}
	stateHash, _ := HashState(values[1])
	now := c.now().UTC()
	a := Attempt{
		ID: values[0], StateHash: stateHash, Binding: binding,
		Environment: c.config.Environment, Issuer: c.config.Issuer, ClientID: c.config.ClientID,
		RedirectURI: c.config.RedirectURI, RequiredACR: c.config.RequiredACR,
		MaxAuthAge: c.config.MaxAuthAge, ClockSkew: c.config.ClockSkew,
		Nonce: values[2], CodeVerifier: values[3], CreatedAt: now, ExpiresAt: now.Add(c.config.AttemptTTL),
	}
	challenge := sha256.Sum256([]byte(a.CodeVerifier))
	claims := map[string]any{"acr": map[string]any{"essential": true, "values": []string{a.RequiredACR}}}
	requestedClaims, err := json.Marshal(map[string]any{"id_token": claims})
	if err != nil {
		return Authorization{}, ErrConfig
	}
	query := url.Values{
		"client_id": {a.ClientID}, "redirect_uri": {a.RedirectURI}, "response_type": {"code"},
		"response_mode": {"query"}, "scope": {"openid"}, "state": {values[1]}, "nonce": {a.Nonce},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
		"max_age": {"0"}, "acr_values": {a.RequiredACR}, "claims": {string(requestedClaims)},
	}
	if err := c.store.Create(ctx, a); err != nil {
		return Authorization{}, ErrStore
	}
	return Authorization{
		AttemptID: a.ID, AuthorizationURL: c.discovery.AuthorizationURL + "?" + query.Encode(),
		ExpiresAt: a.ExpiresAt, Environment: a.Environment, Demo: a.Environment == Sandbox,
	}, nil
}

// Callback accepts parsed query values only after the handler rejects malformed
// URL encoding. Binding must come from authenticated/server-owned state.
func (c *Client) Callback(ctx context.Context, binding Binding, query url.Values) (Evidence, error) {
	if !validBinding(binding) {
		return Evidence{}, ErrBinding
	}
	if len(query["state"]) != 1 {
		return Evidence{}, ErrCallback
	}
	stateHash, err := HashState(query.Get("state"))
	if err != nil {
		return Evidence{}, err
	}
	a, err := c.store.Consume(ctx, stateHash, binding, c.now().UTC())
	if err != nil {
		return Evidence{}, storeError(err)
	}
	if !c.validAttempt(a, binding, stateHash) {
		return Evidence{}, ErrAttemptUnavailable
	}
	for _, values := range query {
		if len(values) != 1 || len(values[0]) > 8192 {
			return Evidence{}, ErrCallback
		}
	}
	if query.Get("iss") != a.Issuer {
		return Evidence{}, ErrCallback
	}
	if query.Has("error") {
		if query.Has("code") || query.Get("error") != "access_denied" {
			return Evidence{}, ErrCallback
		}
		return Evidence{}, ErrDenied
	}
	if !nonblank(query.Get("code"), 8192) {
		return Evidence{}, ErrCallback
	}
	ctx, cancel := context.WithTimeout(ctx, min(10*time.Second, a.ExpiresAt.Sub(c.now())))
	defer cancel()
	idToken, err := c.exchange(ctx, a, query.Get("code"))
	if err != nil {
		return Evidence{}, err
	}
	evidence, err := c.validateToken(ctx, a, idToken)
	if err != nil {
		return Evidence{}, err
	}
	now := c.now().UTC()
	if !now.Before(evidence.ValidUntil) {
		return Evidence{}, ErrAttemptUnavailable
	}
	if err := c.store.Complete(ctx, a.ID, evidence, now); err != nil {
		return Evidence{}, storeError(err)
	}
	return evidence, nil
}

func (c *Client) Cancel(ctx context.Context, attemptID string, binding Binding) error {
	if !validBinding(binding) || !nonblank(attemptID, 256) {
		return ErrBinding
	}
	if err := c.store.Cancel(ctx, attemptID, binding, c.now().UTC()); err != nil {
		return storeError(err)
	}
	return nil
}

func storeError(err error) error {
	if errors.Is(err, ErrAttemptUnavailable) {
		return ErrAttemptUnavailable
	}
	// Store/HTTP errors may include private payloads; return only stable classes.
	return ErrStore
}

func (c *Client) validAttempt(a Attempt, binding Binding, stateHash string) bool {
	now := c.now()
	return a.Binding == binding && a.StateHash == stateHash && nonblank(a.ID, 256) &&
		a.Issuer == c.config.Issuer && a.Environment == c.config.Environment && a.ClientID == c.config.ClientID &&
		a.RedirectURI == c.config.RedirectURI && a.RequiredACR == c.config.RequiredACR &&
		a.MaxAuthAge == c.config.MaxAuthAge && a.ClockSkew == c.config.ClockSkew &&
		validRandom(a.Nonce) && validRandom(a.CodeVerifier) &&
		!a.CreatedAt.After(now.Add(a.ClockSkew)) && a.ExpiresAt.After(a.CreatedAt) &&
		a.ExpiresAt.Sub(a.CreatedAt) <= c.config.AttemptTTL && now.Before(a.ExpiresAt)
}

func validBinding(b Binding) bool {
	if !nonblank(b.AccountID, 256) || !nonblank(b.OrderID, 256) ||
		(b.ExpectedSubject == "") != b.AllowEnrollment ||
		(b.ExpectedSubject != "" && !nonblank(b.ExpectedSubject, 255)) {
		return false
	}
	hash := strings.TrimPrefix(b.IntentHash, "0x")
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == 32 && hash == strings.ToLower(hash)
}

// HashState supports central read-only lookup of the saved callback binding.
// That lookup does not consume state; Consume remains the atomic replay barrier.
func HashState(state string) (string, error) {
	if !validRandom(state) {
		return "", ErrCallback
	}
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:]), nil
}

func validRandom(value string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(value) == 43 && len(decoded) == 32
}

func randomValue() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
