package world

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

type ClientAuthMethod string

const (
	ClientSecretBasic ClientAuthMethod = "client_secret_basic"
	ClientSecretPost  ClientAuthMethod = "client_secret_post"
	PrivateKeyJWT     ClientAuthMethod = "private_key_jwt"
)

// Config is injected by startup; this package never reads environment variables.
type Config struct {
	Enabled       bool
	Environment   Environment
	Issuer        string
	ClientID      string
	RedirectURI   string
	AuthMethod    ClientAuthMethod
	ClientSecret  string `json:"-"`
	PrivateKeyPEM string `json:"-"`
	PrivateKeyID  string
	AttemptTTL    time.Duration
	MaxAuthAge    time.Duration
	ClockSkew     time.Duration
	RequiredACR   string
}

func (Config) String() string   { return "world.Config{private}" }
func (Config) GoString() string { return "world.Config{private}" }

func (c Config) Validate() error {
	_, _, err := normalizeConfig(c)
	return err
}

func normalizeConfig(c Config) (Config, *rsa.PrivateKey, error) {
	if !c.Enabled {
		return c, nil, nil
	}
	if (c.Environment != Sandbox || c.Issuer != SandboxIssuer) &&
		(c.Environment != Production || c.Issuer != ProductionIssuer) {
		return c, nil, fmt.Errorf("%w: issuer/environment mismatch", ErrConfig)
	}
	if !nonblank(c.ClientID, 256) || !validRedirect(c.RedirectURI, c.Environment) {
		return c, nil, fmt.Errorf("%w: client ID or exact redirect", ErrConfig)
	}
	if c.AuthMethod == "" {
		c.AuthMethod = ClientSecretBasic
	}
	if c.AttemptTTL == 0 {
		c.AttemptTTL = 5 * time.Minute
	}
	if c.MaxAuthAge == 0 {
		c.MaxAuthAge = 5 * time.Minute
	}
	if c.ClockSkew == 0 {
		c.ClockSkew = 30 * time.Second
	}
	if c.RequiredACR == "" {
		c.RequiredACR = OrbACR
	}
	if c.AttemptTTL < time.Second || c.AttemptTTL > 5*time.Minute ||
		c.MaxAuthAge < time.Second || c.MaxAuthAge > 5*time.Minute ||
		c.ClockSkew < 0 || c.ClockSkew > 30*time.Second ||
		(c.RequiredACR != OrbACR && c.RequiredACR != OrbFaceACR) {
		return c, nil, fmt.Errorf("%w: freshness or assurance policy", ErrConfig)
	}
	switch c.AuthMethod {
	case ClientSecretBasic, ClientSecretPost:
		if !nonblank(c.ClientSecret, 8192) || c.PrivateKeyPEM != "" || c.PrivateKeyID != "" {
			return c, nil, fmt.Errorf("%w: secret authentication", ErrConfig)
		}
		return c, nil, nil
	case PrivateKeyJWT:
		if c.ClientSecret != "" || !nonblank(c.PrivateKeyID, 256) {
			return c, nil, fmt.Errorf("%w: private key authentication", ErrConfig)
		}
		key, err := parsePrivateKey(c.PrivateKeyPEM)
		if err != nil {
			return c, nil, fmt.Errorf("%w: RSA private key", ErrConfig)
		}
		c.PrivateKeyPEM = ""
		return c, key, nil
	default:
		return c, nil, fmt.Errorf("%w: registered authentication method", ErrConfig)
	}
}

func nonblank(s string, max int) bool {
	return s != "" && len(s) <= max && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}

func validRedirect(raw string, environment Environment) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Host == "" || u.User != nil || u.Fragment != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Opaque != "" || u.Path == "" || u.String() != raw {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return environment == Sandbox && u.Scheme == "http" &&
		(u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))
}

func parsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	if len(raw) > 16384 {
		return nil, ErrConfig
	}
	block, rest := pem.Decode([]byte(raw))
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, ErrConfig
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		var err error
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, ErrConfig
		}
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, ErrConfig
		}
		key, _ = parsed.(*rsa.PrivateKey)
	}
	if key == nil || key.N.BitLen() < 2048 || key.N.BitLen() > 8192 || key.Validate() != nil {
		return nil, ErrConfig
	}
	return key, nil
}
