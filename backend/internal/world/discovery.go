package world

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

type discovery struct {
	Issuer             string   `json:"issuer"`
	AuthorizationURL   string   `json:"authorization_endpoint"`
	TokenURL           string   `json:"token_endpoint"`
	JWKSURL            string   `json:"jwks_uri"`
	ResponseTypes      []string `json:"response_types_supported"`
	ResponseModes      []string `json:"response_modes_supported"`
	GrantTypes         []string `json:"grant_types_supported"`
	Scopes             []string `json:"scopes_supported"`
	SigningAlgorithms  []string `json:"id_token_signing_alg_values_supported"`
	ChallengeMethods   []string `json:"code_challenge_methods_supported"`
	AuthMethods        []string `json:"token_endpoint_auth_methods_supported"`
	AuthSigningMethods []string `json:"token_endpoint_auth_signing_alg_values_supported"`
	ACRs               []string `json:"acr_values_supported"`
	Claims             []string `json:"claims_supported"`
	IssuerResponse     bool     `json:"authorization_response_iss_parameter_supported"`
	ClaimsParameter    bool     `json:"claims_parameter_supported"`
}

func (c *Client) discover(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.Issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return ErrConfig
	}
	var d discovery
	if err := c.requestJSON(req, &d); err != nil {
		return err
	}
	if d.Issuer != c.config.Issuer || !d.IssuerResponse || !d.ClaimsParameter ||
		!providerEndpoint(d.AuthorizationURL, d.Issuer) || !providerEndpoint(d.TokenURL, d.Issuer) ||
		!providerEndpoint(d.JWKSURL, d.Issuer) ||
		!slices.Contains(d.ResponseTypes, "code") || !slices.Contains(d.ResponseModes, "query") ||
		!slices.Contains(d.GrantTypes, "authorization_code") || !slices.Contains(d.Scopes, "openid") ||
		!slices.Contains(d.SigningAlgorithms, "RS256") || !slices.Contains(d.ChallengeMethods, "S256") ||
		!slices.Contains(d.AuthMethods, string(c.config.AuthMethod)) || !slices.Contains(d.ACRs, c.config.RequiredACR) ||
		(c.config.AuthMethod == PrivateKeyJWT && !slices.Contains(d.AuthSigningMethods, "RS256")) {
		return ErrProvider
	}
	for _, claim := range []string{"iss", "sub", "aud", "exp", "iat", "nonce", "auth_time", "acr", "amr"} {
		if !slices.Contains(d.Claims, claim) {
			return ErrProvider
		}
	}
	c.discovery = d
	return nil
}

func providerEndpoint(raw, issuer string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && "https://"+u.Host == issuer && u.User == nil &&
		u.Fragment == "" && u.RawQuery == "" && !u.ForceQuery && u.Opaque == "" && u.Path != ""
}

func (c *Client) requestJSON(req *http.Request, dest any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrProvider
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(data) > 65536 || json.Unmarshal(data, dest) != nil {
		return ErrProvider
	}
	return nil
}

type keyCache struct {
	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	lastAttempt time.Time
	expiresAt   time.Time
}

func (c *Client) signingKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if !nonblank(kid, 256) {
		return nil, ErrToken
	}
	c.keys.mu.Lock()
	defer c.keys.mu.Unlock()
	now := c.now()
	if now.Before(c.keys.expiresAt) && c.keys.keys[kid] != nil {
		return c.keys.keys[kid], nil
	}
	// Bound refresh even for random unknown key IDs and unavailable providers.
	if !c.keys.lastAttempt.IsZero() && now.Before(c.keys.lastAttempt.Add(30*time.Second)) {
		return nil, ErrToken
	}
	c.keys.lastAttempt = now
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.discovery.JWKSURL, nil)
	if err != nil {
		return nil, ErrProvider
	}
	var set struct {
		Keys []struct {
			KTY    string   `json:"kty"`
			KID    string   `json:"kid"`
			Use    string   `json:"use"`
			Alg    string   `json:"alg"`
			KeyOps []string `json:"key_ops"`
			N      string   `json:"n"`
			E      string   `json:"e"`
		} `json:"keys"`
	}
	if err := c.requestJSON(req, &set); err != nil {
		return nil, err
	}
	if len(set.Keys) == 0 || len(set.Keys) > 32 {
		return nil, ErrProvider
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, j := range set.Keys {
		if j.KTY != "RSA" || (j.Use != "" && j.Use != "sig") || (j.Alg != "" && j.Alg != "RS256") ||
			(len(j.KeyOps) > 0 && (len(j.KeyOps) != 1 || j.KeyOps[0] != "verify")) {
			continue
		}
		n, ne := base64.RawURLEncoding.Strict().DecodeString(j.N)
		e, ee := base64.RawURLEncoding.Strict().DecodeString(j.E)
		if !nonblank(j.KID, 256) || keys[j.KID] != nil || ne != nil || ee != nil || len(e) == 0 || len(e) > 4 ||
			len(n) < 256 || len(n) > 1024 || n[0] == 0 || e[0] == 0 || strings.ContainsAny(j.N+j.E, "\r\n") {
			return nil, ErrProvider
		}
		exponent := new(big.Int).SetBytes(e).Int64()
		modulus := new(big.Int).SetBytes(n)
		if exponent < 3 || exponent > 2147483647 || exponent%2 == 0 || modulus.BitLen() < 2048 || modulus.Bit(0) == 0 {
			return nil, ErrProvider
		}
		keys[j.KID] = &rsa.PublicKey{N: modulus, E: int(exponent)}
	}
	if len(keys) == 0 {
		return nil, ErrProvider
	}
	c.keys.keys = keys
	c.keys.expiresAt = now.Add(5 * time.Minute)
	if keys[kid] == nil {
		return nil, ErrToken
	}
	return keys[kid], nil
}
