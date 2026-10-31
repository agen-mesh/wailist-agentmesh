package world

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func (c *Client) exchange(ctx context.Context, attempt Attempt, code string) (string, error) {
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {c.config.ClientID},
		"code": {code}, "redirect_uri": {attempt.RedirectURI}, "code_verifier": {attempt.CodeVerifier},
	}
	switch c.config.AuthMethod {
	case ClientSecretPost:
		form.Set("client_secret", c.config.ClientSecret)
	case PrivateKeyJWT:
		jti, err := randomValue()
		if err != nil {
			return "", ErrProvider
		}
		now := c.now()
		assertion := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
			Issuer: c.config.ClientID, Subject: c.config.ClientID, Audience: jwt.ClaimStrings{c.discovery.TokenURL},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)), ID: jti,
		})
		assertion.Header["kid"] = c.config.PrivateKeyID
		signed, err := assertion.SignedString(c.privateKey)
		if err != nil {
			return "", ErrConfig
		}
		form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
		form.Set("client_assertion", signed)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.discovery.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", ErrConfig
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.config.AuthMethod == ClientSecretBasic {
		req.SetBasicAuth(url.QueryEscape(c.config.ClientID), url.QueryEscape(c.config.ClientSecret))
	}
	var response struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	if err := c.requestJSON(req, &response); err != nil {
		return "", err
	}
	if response.Error != "" || response.IDToken == "" || len(response.IDToken) > 32768 {
		return "", ErrProvider
	}
	return response.IDToken, nil
}

type identityClaims struct {
	jwt.RegisteredClaims
	AuthTime *jwt.NumericDate `json:"auth_time"`
	Nonce    string           `json:"nonce"`
	ACR      string           `json:"acr"`
	AMR      []string         `json:"amr"`
	AZP      string           `json:"azp"`
}

func (c *Client) validateToken(ctx context.Context, attempt Attempt, raw string) (Evidence, error) {
	var claims identityClaims
	now := c.now().UTC()
	token, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodRS256 || t.Header["crit"] != nil {
			return nil, ErrToken
		}
		kid, _ := t.Header["kid"].(string)
		return c.signingKey(ctx, kid)
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(attempt.Issuer),
		jwt.WithAudience(attempt.ClientID), jwt.WithExpirationRequired(), jwt.WithStrictDecoding(),
		jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil || token == nil || !token.Valid {
		if errors.Is(err, ErrProvider) {
			return Evidence{}, ErrProvider
		}
		return Evidence{}, ErrToken
	}
	if !nonblank(claims.Subject, 255) || claims.Nonce != attempt.Nonce || claims.ACR != attempt.RequiredACR ||
		(claims.AZP != "" && claims.AZP != attempt.ClientID) ||
		(len(claims.Audience) > 1 && claims.AZP != attempt.ClientID) ||
		(attempt.Binding.ExpectedSubject != "" && claims.Subject != attempt.Binding.ExpectedSubject) ||
		claims.AuthTime == nil || claims.IssuedAt == nil {
		return Evidence{}, ErrToken
	}
	authTime, issuedAt := claims.AuthTime.Time, claims.IssuedAt.Time
	if authTime.Unix() <= 0 || issuedAt.Unix() <= 0 ||
		authTime.After(now.Add(attempt.ClockSkew)) || issuedAt.After(now.Add(attempt.ClockSkew)) ||
		authTime.Before(attempt.CreatedAt.Add(-attempt.ClockSkew)) ||
		issuedAt.Before(attempt.CreatedAt.Add(-attempt.ClockSkew)) ||
		issuedAt.Before(authTime.Add(-attempt.ClockSkew)) || !issuedAt.Before(claims.ExpiresAt.Time) ||
		!now.Before(authTime.Add(attempt.MaxAuthAge)) {
		return Evidence{}, ErrToken
	}
	kind := authenticationKind(claims.ACR, claims.AMR)
	if kind == "" {
		return Evidence{}, ErrToken
	}
	validUntil := authTime.Add(attempt.MaxAuthAge)
	for _, expires := range []time.Time{attempt.ExpiresAt, claims.ExpiresAt.Time} {
		if expires.Before(validUntil) {
			validUntil = expires
		}
	}
	if !now.Before(validUntil) {
		return Evidence{}, ErrAttemptUnavailable
	}
	return Evidence{
		AttemptID: attempt.ID, Binding: attempt.Binding, Issuer: claims.Issuer, Subject: claims.Subject,
		Environment: attempt.Environment, Demo: attempt.Environment == Sandbox,
		ACR: claims.ACR, AMR: slices.Clone(claims.AMR), AuthenticationKind: kind,
		AuthenticatedAt: authTime.UTC(), TokenIssuedAt: issuedAt.UTC(), TokenExpiresAt: claims.ExpiresAt.Time.UTC(),
		VerifiedAt: now, ValidUntil: validUntil.UTC(),
	}, nil
}

func authenticationKind(acr string, amr []string) string {
	methods := slices.Clone(amr)
	slices.Sort(methods)
	switch {
	case acr == OrbACR && slices.Equal(methods, []string{"iris", "pop"}):
		return "world_proof"
	case acr == OrbACR && slices.Equal(methods, []string{"pop"}):
		return "world_proof"
	case acr == OrbACR && slices.Equal(methods, []string{"mfa", "pop", "user"}):
		return "passkey"
	case acr == OrbFaceACR && slices.Equal(methods, []string{"face", "pop"}):
		return "face_proof"
	default:
		return ""
	}
}
