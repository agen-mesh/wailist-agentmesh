package world

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	f := newIssuer(t, Sandbox)
	if err := (Config{}).Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := New(context.Background(), Config{}, nil, nil); !errors.Is(err, ErrDisabled) {
		t.Fatal("zero config must be disabled")
	}
	for name, change := range map[string]func(*Config){
		"arbitrary issuer":          func(c *Config) { c.Issuer = "https://attacker.example" },
		"insecure issuer":           func(c *Config) { c.Issuer = "http://sandbox.auth.world.org" },
		"cross environment":         func(c *Config) { c.Environment = Production },
		"unknown environment":       func(c *Config) { c.Environment = "demo" },
		"issuer trailing slash":     func(c *Config) { c.Issuer += "/" },
		"missing client":            func(c *Config) { c.ClientID = "" },
		"missing secret":            func(c *Config) { c.ClientSecret = "" },
		"public client":             func(c *Config) { c.AuthMethod = "none" },
		"relative callback":         func(c *Config) { c.RedirectURI = "/callback" },
		"remote plaintext callback": func(c *Config) { c.RedirectURI = "http://app.example/callback" },
		"production plaintext loopback": func(c *Config) {
			c.Environment = Production
			c.Issuer = ProductionIssuer
			c.RedirectURI = "http://localhost/callback"
		},
		"callback fragment":    func(c *Config) { c.RedirectURI += "#fragment" },
		"callback userinfo":    func(c *Config) { c.RedirectURI = "https://user@app.example/callback" },
		"callback query":       func(c *Config) { c.RedirectURI += "?next=anywhere" },
		"callback force query": func(c *Config) { c.RedirectURI += "?" },
		"callback no path":     func(c *Config) { c.RedirectURI = "https://app.example" },
		"ttl too long":         func(c *Config) { c.AttemptTTL = 6 * time.Minute },
		"negative ttl":         func(c *Config) { c.AttemptTTL = -time.Second },
		"age too long":         func(c *Config) { c.MaxAuthAge = 6 * time.Minute },
		"excess skew":          func(c *Config) { c.ClockSkew = time.Minute },
		"unknown assurance":    func(c *Config) { c.RequiredACR = "arbitrary" },
		"mixed credentials":    func(c *Config) { c.PrivateKeyPEM = fixturePrivatePEM() },
		"missing private key":  func(c *Config) { c.AuthMethod = PrivateKeyJWT; c.ClientSecret = ""; c.PrivateKeyID = "fixture-client" },
	} {
		t.Run(name, func(t *testing.T) {
			c := f.cfg
			change(&c)
			if !errors.Is(c.Validate(), ErrConfig) {
				t.Fatal("invalid config accepted")
			}
		})
	}
	for _, redirect := range []string{"http://localhost:8000/callback", "http://127.0.0.1:8000/callback", "http://[::1]:8000/callback", "https://app.example/exact%2Fcallback"} {
		c := f.cfg
		c.RedirectURI = redirect
		if err := c.Validate(); err != nil {
			t.Fatal("valid exact callback rejected:", err)
		}
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", f.cfg, f.cfg), f.cfg.ClientSecret) {
		t.Fatal("config formatting exposes secret")
	}
}

func TestDiscovery(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"issuer":                    func(d map[string]any) { d["issuer"] = ProductionIssuer },
		"foreign token endpoint":    func(d map[string]any) { d["token_endpoint"] = "https://attacker.example/token" },
		"cross environment keys":    func(d map[string]any) { d["jwks_uri"] = ProductionIssuer + "/jwks" },
		"plaintext authorization":   func(d map[string]any) { d["authorization_endpoint"] = "http://sandbox.auth.world.org/authorize" },
		"userinfo in keys endpoint": func(d map[string]any) { d["jwks_uri"] = "https://user@sandbox.auth.world.org/jwks" },
		"implicit only":             func(d map[string]any) { d["response_types_supported"] = []string{"id_token"} },
		"wrong response mode":       func(d map[string]any) { d["response_modes_supported"] = []string{"form_post"} },
		"wrong grant":               func(d map[string]any) { d["grant_types_supported"] = []string{"client_credentials"} },
		"no openid":                 func(d map[string]any) { d["scopes_supported"] = []string{"profile"} },
		"wrong algorithm":           func(d map[string]any) { d["id_token_signing_alg_values_supported"] = []string{"HS256"} },
		"plain PKCE":                func(d map[string]any) { d["code_challenge_methods_supported"] = []string{"plain"} },
		"public auth only":          func(d map[string]any) { d["token_endpoint_auth_methods_supported"] = []string{"none"} },
		"missing response issuer":   func(d map[string]any) { delete(d, "authorization_response_iss_parameter_supported") },
		"no essential claims":       func(d map[string]any) { d["claims_parameter_supported"] = false },
		"no auth_time claim": func(d map[string]any) {
			d["claims_supported"] = []string{"iss", "sub", "aud", "exp", "iat", "nonce", "acr", "amr"}
		},
		"wrong ACR": func(d map[string]any) { d["acr_values_supported"] = []string{"unsupported"} },
	} {
		t.Run(name, func(t *testing.T) {
			f := newIssuer(t, Sandbox)
			f.metadata = change
			_, err := New(context.Background(), f.cfg, newFixtureStore(), f.http)
			if !errors.Is(err, ErrProvider) {
				t.Fatal("invalid discovery accepted")
			}
		})
	}
	t.Run("redirect is not followed", func(t *testing.T) {
		f := newIssuer(t, Sandbox)
		redirect := http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://attacker.example/discovery"}}, Body: http.NoBody, Request: r}, nil
		})}
		if _, err := New(context.Background(), f.cfg, newFixtureStore(), &redirect); !errors.Is(err, ErrProvider) {
			t.Fatal("redirect accepted")
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
