package simulation_test

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/world"
	"github.com/agentmesh/backend/internal/world/simulation"
)

type tokenObservation struct {
	status             int
	idToken, errorCode string
}

type observingTransport struct {
	next http.RoundTripper
	mu   sync.Mutex
	last tokenObservation
}

func (o *observingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := o.next.RoundTrip(r)
	if err != nil || r.URL.Path != "/token" || r.Method != http.MethodPost {
		return response, err
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65536))
	response.Body.Close()
	if err != nil {
		return nil, errors.New("local token observation unavailable")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	var envelope struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return nil, errors.New("invalid local token observation")
	}
	o.mu.Lock()
	o.last = tokenObservation{status: response.StatusCode, idToken: envelope.IDToken, errorCode: envelope.Error}
	o.mu.Unlock()
	return response, nil
}

func (o *observingTransport) snapshot() tokenObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.last
}

func fixturePublicKey(t *testing.T, client *http.Client, issuer string) *rsa.PublicKey {
	t.Helper()
	response, err := client.Get(issuer + "/jwks")
	if err != nil {
		t.Fatal("local JWKS unavailable")
	}
	defer response.Body.Close()
	var jwks struct {
		Keys []struct{ Kty, Kid, N, E string } `json:"keys"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&jwks) != nil || len(jwks.Keys) != 1 {
		t.Fatal("invalid local JWKS")
	}
	key := jwks.Keys[0]
	n, ne := base64.RawURLEncoding.DecodeString(key.N)
	e, ee := base64.RawURLEncoding.DecodeString(key.E)
	if ne != nil || ee != nil || len(e) == 0 || len(e) > 4 || key.Kty != "RSA" || key.Kid != "local-simulation-key" {
		t.Fatal("invalid local signing key")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
}

func assertObservedToken(t *testing.T, outcome simulation.Outcome, observation tokenObservation, key *rsa.PublicKey, clientID, nonce string) {
	t.Helper()
	if outcome == simulation.Denied {
		if observation != (tokenObservation{}) {
			t.Fatal("denied authorization performed a token request")
		}
		return
	}
	if outcome == simulation.TokenError {
		if observation.status != http.StatusBadRequest || observation.errorCode != "invalid_grant" || observation.idToken != "" {
			t.Fatal("token error did not return the expected protocol failure")
		}
		return
	}
	if observation.status != http.StatusOK || observation.errorCode != "" {
		t.Fatal("claim fault did not produce a token response")
	}
	parts := strings.Split(observation.idToken, ".")
	if len(parts) != 3 {
		t.Fatal("local token is not a compact JWT")
	}
	header, he := base64.RawURLEncoding.DecodeString(parts[0])
	payload, pe := base64.RawURLEncoding.DecodeString(parts[1])
	signature, se := base64.RawURLEncoding.DecodeString(parts[2])
	if he != nil || pe != nil || se != nil {
		t.Fatal("local token encoding is invalid")
	}
	var algorithm struct{ Alg, Kid string }
	var claims map[string]any
	if json.Unmarshal(header, &algorithm) != nil || json.Unmarshal(payload, &claims) != nil || algorithm.Alg != "RS256" || algorithm.Kid != "local-simulation-key" {
		t.Fatal("local token does not identify the published RSA key")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	signatureError := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature)
	if outcome == simulation.BadSignature {
		if signatureError == nil {
			t.Fatal("bad-signature mode returned a valid signature")
		}
	} else if signatureError != nil {
		t.Fatal("claim rejection was masked by an invalid signature")
	}
	expectedIssuer, expectedAudience, expectedNonce := world.SandboxIssuer, clientID, nonce
	switch outcome {
	case simulation.Success, simulation.Stale, simulation.Expired, simulation.MissingAuthTime, simulation.FutureAuth, simulation.BadSignature:
	case simulation.WrongAudience:
		expectedAudience = "local-simulation-other-client"
	case simulation.WrongNonce:
		expectedNonce = "local-simulation-wrong-nonce"
	case simulation.WrongIssuer:
		expectedIssuer = world.ProductionIssuer
	default:
		t.Fatal("advertised outcome lacks an independent claim check")
	}
	if claims["iss"] != expectedIssuer || claims["aud"] != expectedAudience || claims["nonce"] != expectedNonce || claims["sub"] != simulation.Subject {
		t.Fatal("issuer fault did not preserve the intended claim bindings")
	}
	now := float64(time.Now().Unix())
	expiresAt, ok := claims["exp"].(float64)
	if !ok || (outcome == simulation.Expired && expiresAt >= now) || (outcome != simulation.Expired && expiresAt <= now) {
		t.Fatal("token expiration does not match the selected fault")
	}
	issuedAt, ok := claims["iat"].(float64)
	if !ok || issuedAt < now-60 || issuedAt > now+60 {
		t.Fatal("fault fixture did not issue a new token")
	}
	if outcome == simulation.MissingAuthTime {
		if _, present := claims["auth_time"]; present {
			t.Fatal("missing-auth-time fault still included authentication time")
		}
		return
	}
	authTime, ok := claims["auth_time"].(float64)
	if !ok {
		t.Fatal("local token omitted authentication time unexpectedly")
	}
	switch outcome {
	case simulation.Stale:
		if authTime >= now-600 {
			t.Fatal("stale fault did not use old authentication")
		}
	case simulation.FutureAuth:
		if authTime <= now+600 {
			t.Fatal("future-auth fault did not use future authentication")
		}
	default:
		if authTime < now-60 || authTime > now+60 {
			t.Fatal("claim fault changed authentication time unexpectedly")
		}
	}
}
