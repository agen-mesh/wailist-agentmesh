package world

import (
	"context"
	"errors"
	"time"
)

type Environment string

const (
	Production       Environment = "production"
	Sandbox          Environment = "sandbox"
	ProductionIssuer             = "https://auth.world.org"
	SandboxIssuer                = "https://sandbox.auth.world.org"
	OrbACR                       = "https://world.org/oidc/acr/orb"
	OrbFaceACR                   = "https://world.org/oidc/acr/orb-face"
)

var (
	ErrDisabled           = errors.New("world: integration disabled")
	ErrConfig             = errors.New("world: invalid private configuration")
	ErrBinding            = errors.New("world: invalid account or intent binding")
	ErrAttemptUnavailable = errors.New("world: attempt unavailable")
	ErrCallback           = errors.New("world: invalid authorization response")
	ErrDenied             = errors.New("world: authentication denied")
	ErrProvider           = errors.New("world: provider unavailable or invalid response")
	ErrToken              = errors.New("world: invalid authentication evidence")
	ErrStore              = errors.New("world: durable storage unavailable")
)

// Binding must come from the authenticated session and immutable server-side order.
// Enrollment is explicit; an existing link always requires ExpectedSubject.
type Binding struct {
	AccountID       string
	OrderID         string
	IntentHash      string
	ExpectedSubject string `json:"-"`
	AllowEnrollment bool
}

func (Binding) String() string   { return "world.Binding{private}" }
func (Binding) GoString() string { return "world.Binding{private}" }

// Attempt is private durable state. Encrypt Nonce and CodeVerifier at rest.
// StateHash is SHA-256 of the browser state, encoded as lowercase hexadecimal.
type Attempt struct {
	ID           string
	StateHash    string
	Binding      Binding
	Environment  Environment
	Issuer       string
	ClientID     string
	RedirectURI  string
	RequiredACR  string
	MaxAuthAge   time.Duration
	ClockSkew    time.Duration
	Nonce        string `json:"-"`
	CodeVerifier string `json:"-"`
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

func (Attempt) String() string   { return "world.Attempt{private}" }
func (Attempt) GoString() string { return "world.Attempt{private}" }

// Store must enforce these transitions atomically in durable storage across all
// instances. There is deliberately no production in-memory implementation.
type Store interface {
	// Create inserts a unique ID and StateHash, freezing the entire attempt.
	Create(context.Context, Attempt) error
	// Consume compares the full Binding and expiry, then moves pending to consumed
	// once. Unknown, mismatched, expired or terminal attempts return
	// ErrAttemptUnavailable. No failure may restore pending or extend expiry.
	Consume(context.Context, string, Binding, time.Time) (Attempt, error)
	// Complete records evidence only for a consumed, unexpired, uncancelled attempt
	// with identical binding. It must serialize with Cancel and central order
	// invalidation. A second completion returns ErrAttemptUnavailable. It grants
	// no payment approval. Recheck subject ownership/enrollment atomically here.
	Complete(context.Context, string, Evidence, time.Time) error
	// Cancel terminally invalidates pending or consumed attempts matching Binding.
	// It serializes with Complete; completed evidence must still be revocable by
	// the central order policy before any separately approved action executes.
	Cancel(context.Context, string, Binding, time.Time) error
}

type Authorization struct {
	AttemptID        string      `json:"attempt_id"`
	AuthorizationURL string      `json:"authorization_url"`
	ExpiresAt        time.Time   `json:"expires_at"`
	Environment      Environment `json:"environment"`
	Demo             bool        `json:"demo"`
}

// Evidence is private backend data, not an application token or action approval.
// Consumers must recheck ValidUntil and the durable attempt/order at execution.
type Evidence struct {
	AttemptID          string
	Binding            Binding
	Issuer             string
	Subject            string `json:"-"`
	Environment        Environment
	Demo               bool
	ACR                string
	AMR                []string
	AuthenticationKind string
	AuthenticatedAt    time.Time
	TokenIssuedAt      time.Time
	TokenExpiresAt     time.Time
	VerifiedAt         time.Time
	ValidUntil         time.Time
}

func (Evidence) String() string   { return "world.Evidence{private}" }
func (Evidence) GoString() string { return "world.Evidence{private}" }
