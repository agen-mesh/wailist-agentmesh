package world

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAttempt(t *testing.T) {
	for name, change := range map[string]func(*Binding){
		"missing account":      func(b *Binding) { b.AccountID = "" },
		"missing order":        func(b *Binding) { b.OrderID = "" },
		"unfrozen intent":      func(b *Binding) { b.IntentHash = "anything" },
		"uppercase intent":     func(b *Binding) { b.IntentHash = strings.Repeat("A", 64) },
		"implicit enrollment":  func(b *Binding) { b.ExpectedSubject = "" },
		"relinking enrollment": func(b *Binding) { b.AllowEnrollment = true },
	} {
		t.Run(name, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			b := fixtureBinding()
			change(&b)
			if _, err := c.Begin(context.Background(), b); !errors.Is(err, ErrBinding) {
				t.Fatal("unbound attempt created")
			}
			if len(s.rows) != 0 {
				t.Fatal("invalid binding persisted")
			}
		})
	}
	for name, change := range map[string]func(*Binding){
		"account":           func(b *Binding) { b.AccountID = "account-2" },
		"order":             func(b *Binding) { b.OrderID = "order-2" },
		"intent":            func(b *Binding) { b.IntentHash = strings.Repeat("b", 64) },
		"subject policy":    func(b *Binding) { b.ExpectedSubject = "another-subject" },
		"enrollment policy": func(b *Binding) { b.ExpectedSubject = ""; b.AllowEnrollment = true },
	} {
		t.Run("changed "+name, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			b := fixtureBinding()
			_, q := beginFixture(t, f, c, b)
			change(&b)
			e, err := c.Callback(context.Background(), b, q)
			assertNoEvidence(t, e, err)
			if f.exchanges != 0 {
				t.Fatal("wrong binding exchanged")
			}
			if _, err := c.Callback(context.Background(), fixtureBinding(), q); err != nil {
				t.Fatal("wrong account burned owner attempt")
			}
		})
	}
	t.Run("expired", func(t *testing.T) {
		f := newIssuer(t, Sandbox)
		s := newFixtureStore()
		c := f.client(t, s)
		a, q := beginFixture(t, f, c, fixtureBinding())
		f.now = a.ExpiresAt
		e, err := c.Callback(context.Background(), fixtureBinding(), q)
		assertNoEvidence(t, e, err)
		if f.exchanges != 0 || s.completed != 0 {
			t.Fatal("expired attempt reached token endpoint")
		}
	})
	t.Run("execution freshness bound", func(t *testing.T) {
		f := newIssuer(t, Sandbox)
		f.cfg.MaxAuthAge = time.Minute
		s := newFixtureStore()
		c := f.client(t, s)
		original := f.now
		_, q := beginFixture(t, f, c, fixtureBinding())
		f.now = f.now.Add(time.Minute)
		f.payload = func(p map[string]any) { p["auth_time"] = original.Unix() }
		e, err := c.Callback(context.Background(), fixtureBinding(), q)
		assertNoEvidence(t, e, err)
	})
	for name, corrupt := range map[string]func(*Attempt){
		"issuer":            func(a *Attempt) { a.Issuer = ProductionIssuer },
		"environment":       func(a *Attempt) { a.Environment = Production },
		"client":            func(a *Attempt) { a.ClientID = "other" },
		"redirect":          func(a *Attempt) { a.RedirectURI += "/changed" },
		"account":           func(a *Attempt) { a.Binding.AccountID = "other" },
		"state":             func(a *Attempt) { a.StateHash = "other" },
		"policy":            func(a *Attempt) { a.RequiredACR = OrbFaceACR },
		"age":               func(a *Attempt) { a.MaxAuthAge = time.Hour },
		"skew":              func(a *Attempt) { a.ClockSkew = time.Hour },
		"extended lifetime": func(a *Attempt) { a.ExpiresAt = a.ExpiresAt.Add(time.Minute) },
		"future creation":   func(a *Attempt) { a.CreatedAt = a.CreatedAt.Add(time.Hour) },
	} {
		t.Run("corrupt store "+name, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			s.corrupt = corrupt
			c := f.client(t, s)
			_, q := beginFixture(t, f, c, fixtureBinding())
			e, err := c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
			if f.exchanges != 0 {
				t.Fatal("corrupt persisted attempt exchanged")
			}
		})
	}
}

func TestReplay(t *testing.T) {
	for _, mode := range []string{"wrong verifier", "wrong saved nonce", "code from other attempt", "redeemed code"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			_, q := beginFixture(t, f, c, fixtureBinding())
			expectedCompletions := 0
			switch mode {
			case "wrong verifier":
				s.corrupt = func(a *Attempt) { a.CodeVerifier = strings.Repeat("A", 43) }
			case "wrong saved nonce":
				s.corrupt = func(a *Attempt) { a.Nonce = strings.Repeat("A", 43) }
			case "code from other attempt":
				_, other := beginFixture(t, f, c, fixtureBinding())
				q.Set("code", other.Get("code"))
			case "redeemed code":
				if _, err := c.Callback(context.Background(), fixtureBinding(), q); err != nil {
					t.Fatal(err)
				}
				oldCode := q.Get("code")
				_, q = beginFixture(t, f, c, fixtureBinding())
				q.Set("code", oldCode)
				expectedCompletions = 1
			}
			e, err := c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
			if s.completed != expectedCompletions {
				t.Fatal("replay persisted evidence")
			}
		})
	}
	for _, mode := range []string{"unknown", "malformed", "duplicate", "missing"} {
		t.Run("state "+mode, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			_, q := beginFixture(t, f, c, fixtureBinding())
			original := q.Encode()
			switch mode {
			case "unknown":
				q.Set("state", strings.Repeat("A", 43))
			case "malformed":
				q.Set("state", "malformed")
			case "duplicate":
				q.Add("state", q.Get("state"))
			case "missing":
				q.Del("state")
			}
			e, err := c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
			if f.exchanges != 0 {
				t.Fatal("invalid state exchanged")
			}
			q, _ = url.ParseQuery(original)
			if _, err := c.Callback(context.Background(), fixtureBinding(), q); err != nil {
				t.Fatal("unknown state consumed original")
			}
		})
	}
}

func TestStore(t *testing.T) {
	privateFailure := errors.New("secret-marker-private-store-payload")
	for _, stage := range []string{"create", "consume", "complete"} {
		t.Run(stage, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			if stage == "create" {
				s.createErr = privateFailure
				a, err := c.Begin(context.Background(), fixtureBinding())
				if !errors.Is(err, ErrStore) || a.AuthorizationURL != "" || strings.Contains(err.Error(), "secret-marker") {
					t.Fatal("store create error leaked or succeeded")
				}
				return
			}
			_, q := beginFixture(t, f, c, fixtureBinding())
			if stage == "consume" {
				s.consumeErr = privateFailure
			} else {
				s.completeErr = privateFailure
			}
			e, err := c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
			if !errors.Is(err, ErrStore) || strings.Contains(err.Error(), "secret-marker") || s.completed != 0 {
				t.Fatal("store error not sanitized")
			}
			if stage == "complete" {
				s.completeErr = nil
				e, err = c.Callback(context.Background(), fixtureBinding(), q)
				assertNoEvidence(t, e, err)
				if f.exchanges != 1 {
					t.Fatal("completion failure retried")
				}
			}
		})
	}
	t.Run("provider payload privacy", func(t *testing.T) {
		f := newIssuer(t, Sandbox)
		s := newFixtureStore()
		c := f.client(t, s)
		_, q := beginFixture(t, f, c, fixtureBinding())
		base := c.http.Transport
		c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/token" {
				return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant","error_description":"private-token-marker"}`)), Header: make(http.Header), Request: r}, nil
			}
			return base.RoundTrip(r)
		})
		e, err := c.Callback(context.Background(), fixtureBinding(), q)
		assertNoEvidence(t, e, err)
		if strings.Contains(err.Error(), "private-token-marker") {
			t.Fatal("provider body leaked")
		}
	})
	t.Run("evidence excludes credentials", func(t *testing.T) {
		f := newIssuer(t, Sandbox)
		s := newFixtureStore()
		c := f.client(t, s)
		a, q := beginFixture(t, f, c, fixtureBinding())
		e, err := c.Callback(context.Background(), fixtureBinding(), q)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(e)
		start, _ := json.Marshal(a)
		for _, secret := range []string{f.cfg.ClientSecret, "fixture-subject", "fixture-opaque-token"} {
			if strings.Contains(string(data), secret) || strings.Contains(string(start), secret) {
				t.Fatal("private value in response serialization")
			}
		}
	})
}

func TestEnvironment(t *testing.T) {
	for _, environment := range []Environment{Sandbox, Production} {
		t.Run(string(environment), func(t *testing.T) {
			f := newIssuer(t, environment)
			s := newFixtureStore()
			c := f.client(t, s)
			_, q := beginFixture(t, f, c, fixtureBinding())
			e, err := c.Callback(context.Background(), fixtureBinding(), q)
			if err != nil || e.Environment != environment || e.Demo != (environment == Sandbox) {
				t.Fatal("fixture environment incorrectly labelled")
			}
		})
	}
	t.Run("attempt cannot cross environments", func(t *testing.T) {
		s := newFixtureStore()
		sandbox := newIssuer(t, Sandbox)
		production := newIssuer(t, Production)
		a := sandbox.client(t, s)
		b := production.client(t, s)
		_, q := beginFixture(t, sandbox, a, fixtureBinding())
		e, err := b.Callback(context.Background(), fixtureBinding(), q)
		assertNoEvidence(t, e, err)
		if production.exchanges != 0 || sandbox.exchanges != 0 || s.completed != 0 {
			t.Fatal("environment crossed")
		}
	})
}

func TestCallbackProviderFailure(t *testing.T) {
	for _, mode := range []string{"unavailable", "expired code", "registration mismatch", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			_, q := beginFixture(t, f, c, fixtureBinding())
			switch mode {
			case "unavailable":
				f.status = http.StatusServiceUnavailable
			case "expired code":
				f.mu.Lock()
				g := f.codes[q.Get("code")]
				g.expiresAt = f.now
				f.codes[q.Get("code")] = g
				f.mu.Unlock()
			case "registration mismatch":
				f.cfg.AuthMethod = ClientSecretPost
			case "timeout":
				f.started = make(chan struct{})
				f.release = make(chan struct{})
				c.http.Timeout = 50 * time.Millisecond
			}
			e, err := c.Callback(context.Background(), fixtureBinding(), q)
			if mode == "timeout" {
				close(f.release)
			}
			assertNoEvidence(t, e, err)
			if !errors.Is(err, ErrProvider) || s.completed != 0 {
				t.Fatal("dependency failure authorized evidence")
			}
			e, err = c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
			if !errors.Is(err, ErrAttemptUnavailable) {
				t.Fatal("dependency failure reopened attempt")
			}
		})
	}
}

func TestKeyRotation(t *testing.T) {
	f := newIssuer(t, Sandbox)
	s := newFixtureStore()
	c := f.client(t, s)
	_, q := beginFixture(t, f, c, fixtureBinding())
	if _, err := c.Callback(context.Background(), fixtureBinding(), q); err != nil {
		t.Fatal(err)
	}
	f.key = fixtureKeys.other
	f.kid = "rotated-key"
	f.keys = append(f.keys, fixtureJWK(f.key, f.kid))
	_, q = beginFixture(t, f, c, fixtureBinding())
	e, err := c.Callback(context.Background(), fixtureBinding(), q)
	assertNoEvidence(t, e, err)
	if f.jwksReads != 1 {
		t.Fatal("unknown key bypassed refresh bound")
	}
	f.now = f.now.Add(31 * time.Second)
	_, q = beginFixture(t, f, c, fixtureBinding())
	if _, err := c.Callback(context.Background(), fixtureBinding(), q); err != nil {
		t.Fatal("rotated key rejected:", err)
	}
	f.key = fixtureKeys.issuer
	f.kid = "fixture-issuer-key"
	_, q = beginFixture(t, f, c, fixtureBinding())
	if _, err := c.Callback(context.Background(), fixtureBinding(), q); err != nil {
		t.Fatal("overlap key rejected:", err)
	}
	if f.jwksReads != 2 {
		t.Fatal("JWKS cache not reused")
	}
	f.now = f.now.Add(5 * time.Minute)
	f.keySet = func(k map[string]any) { k["keys"] = []any{} }
	_, q = beginFixture(t, f, c, fixtureBinding())
	e, err = c.Callback(context.Background(), fixtureBinding(), q)
	assertNoEvidence(t, e, err)
	if f.jwksReads != 3 {
		t.Fatal("expired key cache did not refresh")
	}
}

func TestProtocolSuccess(t *testing.T) {
	for _, kind := range []string{"world_proof", "passkey", "face_proof", "explicit_enrollment"} {
		t.Run(kind, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			b := fixtureBinding()
			if kind == "passkey" {
				f.payload = func(p map[string]any) { p["amr"] = []string{"pop", "user", "mfa"} }
			}
			if kind == "face_proof" {
				f.cfg.RequiredACR = OrbFaceACR
				f.payload = func(p map[string]any) { p["acr"] = OrbFaceACR; p["amr"] = []string{"pop", "face"} }
			}
			if kind == "explicit_enrollment" {
				b.ExpectedSubject = ""
				b.AllowEnrollment = true
			}
			c := f.client(t, s)
			a, q := beginFixture(t, f, c, b)
			u, _ := url.Parse(a.AuthorizationURL)
			state := u.Query().Get("state")
			stateHash, _ := HashState(state)
			saved := s.rows[stateHash].attempt
			if saved.StateHash == state || strings.Contains(a.AuthorizationURL, saved.CodeVerifier) || strings.Contains(a.AuthorizationURL, "fixture-subject") {
				t.Fatal("private state leaked into authorization URL")
			}
			if !a.Demo || a.Environment != Sandbox || a.ExpiresAt.Sub(saved.CreatedAt) != 5*time.Minute {
				t.Fatal("incorrect public attempt policy")
			}
			e, err := c.Callback(context.Background(), b, q)
			if err != nil {
				t.Fatal("protocol exchange:", err)
			}
			if e.AttemptID != a.AttemptID || e.Binding != b || e.Subject != "fixture-subject" || e.Issuer != SandboxIssuer || !e.Demo || e.Environment != Sandbox || !e.AuthenticatedAt.Equal(f.now) || s.completed != 1 || f.exchanges != 1 || f.jwksReads != 1 {
				t.Fatal("incorrect validated binding or evidence")
			}
			if kind != "explicit_enrollment" && e.AuthenticationKind != kind {
				t.Fatal("authentication method misrepresented")
			}
			if !e.ValidUntil.Equal(a.ExpiresAt) {
				t.Fatal("evidence outlives attempt")
			}
			replay, replayErr := c.Callback(context.Background(), b, q)
			assertNoEvidence(t, replay, replayErr)
			if f.exchanges != 1 || s.completed != 1 {
				t.Fatal("replay reached exchange or completion")
			}
		})
	}
}

func TestClientAuthentication(t *testing.T) {
	for _, method := range []ClientAuthMethod{ClientSecretBasic, ClientSecretPost, PrivateKeyJWT} {
		t.Run(string(method), func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			f.cfg.AuthMethod = method
			if method == PrivateKeyJWT {
				f.cfg.ClientSecret = ""
				f.cfg.PrivateKeyPEM = fixturePrivatePEM()
				f.cfg.PrivateKeyID = "fixture-client-key"
			}
			s := newFixtureStore()
			c := f.client(t, s)
			for range 2 {
				_, q := beginFixture(t, f, c, fixtureBinding())
				if _, err := c.Callback(context.Background(), fixtureBinding(), q); err != nil {
					t.Fatal("registered auth method rejected:", err)
				}
			}
			if s.completed != 2 || (method == PrivateKeyJWT && len(f.jtis) != 2) {
				t.Fatal("assertion reuse or missing completion")
			}
		})
	}
}

func TestTokenRejection(t *testing.T) {
	cases := map[string]func(*issuerFixture){
		"issuer":           func(f *issuerFixture) { f.payload = func(p map[string]any) { p["iss"] = ProductionIssuer } },
		"audience":         func(f *issuerFixture) { f.payload = func(p map[string]any) { p["aud"] = "other-client" } },
		"missing audience": func(f *issuerFixture) { f.payload = func(p map[string]any) { delete(p, "aud") } },
		"multiple audiences without azp": func(f *issuerFixture) {
			f.payload = func(p map[string]any) { p["aud"] = []string{f.cfg.ClientID, "other"} }
		},
		"wrong azp":      func(f *issuerFixture) { f.payload = func(p map[string]any) { p["azp"] = "other-client" } },
		"expiry exact":   func(f *issuerFixture) { f.payload = func(p map[string]any) { p["exp"] = f.now.Unix() } },
		"missing expiry": func(f *issuerFixture) { f.payload = func(p map[string]any) { delete(p, "exp") } },
		"future nbf": func(f *issuerFixture) {
			f.payload = func(p map[string]any) { p["nbf"] = f.now.Add(time.Minute).Unix() }
		},
		"missing issuance": func(f *issuerFixture) { f.payload = func(p map[string]any) { delete(p, "iat") } },
		"future issuance": func(f *issuerFixture) {
			f.payload = func(p map[string]any) { p["iat"] = f.now.Add(time.Minute).Unix() }
		},
		"fresh token stale auth": func(f *issuerFixture) {
			f.payload = func(p map[string]any) { p["auth_time"] = f.now.Add(-time.Hour).Unix() }
		},
		"auth before attempt": func(f *issuerFixture) {
			f.payload = func(p map[string]any) { p["auth_time"] = f.now.Add(-31 * time.Second).Unix() }
		},
		"missing auth time": func(f *issuerFixture) { f.payload = func(p map[string]any) { delete(p, "auth_time") } },
		"future auth": func(f *issuerFixture) {
			f.payload = func(p map[string]any) { p["auth_time"] = f.now.Add(time.Minute).Unix() }
		},
		"nonce":               func(f *issuerFixture) { f.payload = func(p map[string]any) { p["nonce"] = "wrong" } },
		"missing nonce":       func(f *issuerFixture) { f.payload = func(p map[string]any) { delete(p, "nonce") } },
		"subject":             func(f *issuerFixture) { f.payload = func(p map[string]any) { p["sub"] = "other-subject" } },
		"empty subject":       func(f *issuerFixture) { f.payload = func(p map[string]any) { p["sub"] = "" } },
		"missing subject":     func(f *issuerFixture) { f.payload = func(p map[string]any) { delete(p, "sub") } },
		"wrong ACR":           func(f *issuerFixture) { f.payload = func(p map[string]any) { p["acr"] = OrbFaceACR } },
		"missing ACR":         func(f *issuerFixture) { f.payload = func(p map[string]any) { delete(p, "acr") } },
		"wrong AMR":           func(f *issuerFixture) { f.payload = func(p map[string]any) { p["amr"] = []string{"pwd"} } },
		"missing AMR":         func(f *issuerFixture) { f.payload = func(p map[string]any) { delete(p, "amr") } },
		"partial passkey":     func(f *issuerFixture) { f.payload = func(p map[string]any) { p["amr"] = []string{"pop", "mfa"} } },
		"duplicate AMR":       func(f *issuerFixture) { f.payload = func(p map[string]any) { p["amr"] = []string{"pop", "pop"} } },
		"wrong signing key":   func(f *issuerFixture) { f.key = fixtureKeys.other },
		"wrong key ID":        func(f *issuerFixture) { f.kid = "unknown" },
		"missing key ID":      func(f *issuerFixture) { f.header = func(h map[string]any) { delete(h, "kid") } },
		"algorithm confusion": func(f *issuerFixture) { f.header = func(h map[string]any) { h["alg"] = "HS256" } },
		"unsigned algorithm":  func(f *issuerFixture) { f.header = func(h map[string]any) { h["alg"] = "none" } },
		"critical extension":  func(f *issuerFixture) { f.header = func(h map[string]any) { h["crit"] = []string{"unknown"} } },
		"duplicate key ID":    func(f *issuerFixture) { f.keys = append(f.keys, fixtureJWK(fixtureKeys.other, f.kid)) },
		"encryption key":      func(f *issuerFixture) { f.keys[0]["use"] = "enc" },
		"wrong JWK algorithm": func(f *issuerFixture) { f.keys[0]["alg"] = "RS512" },
		"sign-only key":       func(f *issuerFixture) { f.keys[0]["key_ops"] = []string{"sign"} },
		"malformed modulus":   func(f *issuerFixture) { f.keys[0]["n"] = "AA" },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			_, q := beginFixture(t, f, c, fixtureBinding())
			setup(f)
			e, err := c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
			if s.completed != 0 {
				t.Fatal("invalid token persisted")
			}
			e, err = c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
			if f.exchanges != 1 {
				t.Fatal("failed validation reopened attempt")
			}
		})
	}
}

func TestCallback(t *testing.T) {
	for name, change := range map[string]func(url.Values){
		"missing issuer":              func(q url.Values) { q.Del("iss") },
		"wrong issuer":                func(q url.Values) { q.Set("iss", ProductionIssuer) },
		"duplicate issuer":            func(q url.Values) { q.Add("iss", q.Get("iss")) },
		"duplicate code":              func(q url.Values) { q.Add("code", q.Get("code")) },
		"duplicate unknown field":     func(q url.Values) { q["extra"] = []string{"a", "b"} },
		"missing code":                func(q url.Values) { q.Del("code") },
		"denied and code":             func(q url.Values) { q.Set("error", "access_denied") },
		"unknown authorization error": func(q url.Values) { q.Del("code"); q.Set("error", "login_required") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			_, q := beginFixture(t, f, c, fixtureBinding())
			original := q.Encode()
			change(q)
			e, err := c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
			if f.exchanges != 0 || s.completed != 0 {
				t.Fatal("invalid callback reached provider")
			}
			q, _ = url.ParseQuery(original)
			e, err = c.Callback(context.Background(), fixtureBinding(), q)
			assertNoEvidence(t, e, err)
		})
	}
	t.Run("denial", func(t *testing.T) {
		f := newIssuer(t, Sandbox)
		f.denied = true
		s := newFixtureStore()
		c := f.client(t, s)
		_, q := beginFixture(t, f, c, fixtureBinding())
		e, err := c.Callback(context.Background(), fixtureBinding(), q)
		assertNoEvidence(t, e, err)
		if !errors.Is(err, ErrDenied) || f.exchanges != 0 || s.completed != 0 {
			t.Fatal("denial misclassified")
		}
	})
}

func TestConcurrent(t *testing.T) {
	f := newIssuer(t, Sandbox)
	s := newFixtureStore()
	first := f.client(t, s)
	second := f.client(t, s)
	_, q := beginFixture(t, f, first, fixtureBinding())
	start := make(chan struct{})
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c := first
			if i%2 == 1 {
				c = second
			}
			_, err := c.Callback(context.Background(), fixtureBinding(), q)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrAttemptUnavailable) {
			t.Fatal("unexpected concurrency result:", err)
		}
	}
	if successes != 1 || s.completed != 1 || f.exchanges != 1 {
		t.Fatal("concurrent callbacks were not single use")
	}
	third := f.client(t, s)
	e, err := third.Callback(context.Background(), fixtureBinding(), q)
	assertNoEvidence(t, e, err)
}

func TestCancel(t *testing.T) {
	for _, inflight := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "during exchange"}[inflight], func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			s := newFixtureStore()
			c := f.client(t, s)
			a, q := beginFixture(t, f, c, fixtureBinding())
			if !inflight {
				if err := c.Cancel(context.Background(), a.AttemptID, fixtureBinding()); err != nil {
					t.Fatal(err)
				}
				e, err := c.Callback(context.Background(), fixtureBinding(), q)
				assertNoEvidence(t, e, err)
				if f.exchanges != 0 {
					t.Fatal("cancelled attempt exchanged")
				}
				return
			}
			f.started = make(chan struct{})
			f.release = make(chan struct{})
			result := make(chan error, 1)
			go func() {
				e, err := c.Callback(context.Background(), fixtureBinding(), q)
				if e.AttemptID != "" {
					result <- errors.New("cancelled evidence returned")
					return
				}
				result <- err
			}()
			<-f.started
			if err := c.Cancel(context.Background(), a.AttemptID, fixtureBinding()); err != nil {
				t.Fatal(err)
			}
			close(f.release)
			if !errors.Is(<-result, ErrAttemptUnavailable) || s.completed != 0 {
				t.Fatal("inflight cancellation lost")
			}
		})
	}
}
