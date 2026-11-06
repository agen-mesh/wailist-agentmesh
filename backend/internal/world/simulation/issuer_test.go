package simulation_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/world"
	"github.com/agentmesh/backend/internal/world/simulation"
)

func TestControlledApplicationIssuer(t *testing.T) {
	issuer, err := simulation.New("http://127.0.0.1:8000/purchases/world/callback")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(issuer.Close)
	registration := issuer.Registration()
	if registration.Issuer != world.SandboxIssuer {
		t.Fatal("simulator selected a live environment")
	}
	if issuer.Observations().TokenRequests != 0 {
		t.Fatal("new issuer has token requests")
	}
	publicKey := fixturePublicKey(t, issuer.HTTPClient(), registration.Issuer)
	for _, outcome := range simulation.SupportedOutcomes() {
		t.Run(string(outcome), func(t *testing.T) {
			before := issuer.Observations().TokenRequests
			if err := issuer.SetOutcome(outcome); err != nil {
				t.Fatal(err)
			}
			if issuer.Observations().TokenRequests != before {
				t.Fatal("outcome change erased token request history")
			}
			store := &memoryStore{rows: make(map[string]*row)}
			httpClient := issuer.HTTPClient()
			observer := &observingTransport{next: httpClient.Transport}
			httpClient.Transport = observer
			client, err := world.New(context.Background(), world.Config{Enabled: true, Environment: world.Sandbox, Issuer: registration.Issuer,
				ClientID: registration.ClientID, ClientSecret: registration.ClientSecret, RedirectURI: registration.RedirectURI}, store, httpClient)
			if err != nil {
				t.Fatal(err)
			}
			binding := world.Binding{AccountID: "synthetic-account", OrderID: "synthetic-order", IntentHash: strings.Repeat("a", 64), ExpectedSubject: simulation.Subject}
			auth, err := client.Begin(context.Background(), binding)
			if err != nil {
				t.Fatal(err)
			}
			browserURL, err := issuer.BrowserURL(auth.AuthorizationURL)
			if err != nil {
				t.Fatal(err)
			}
			browser := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, err := browser.Get(browserURL)
			if err != nil {
				t.Fatal("local browser request failed")
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusFound {
				t.Fatal("local issuer did not return a callback")
			}
			callback, err := url.Parse(response.Header.Get("Location"))
			if err != nil {
				t.Fatal("invalid local callback")
			}
			if err := issuer.SetOutcome(simulation.Success); err != nil {
				t.Fatal(err)
			}
			evidence, err := client.Callback(context.Background(), binding, callback.Query())
			switch outcome {
			case simulation.Success:
				if err != nil || !evidence.Demo || evidence.Environment != world.Sandbox || evidence.Subject != simulation.Subject || !store.completed {
					t.Fatal("real client rejected controlled success")
				}
				if evidence.ACR != world.OrbACR || evidence.AuthenticationKind != "world_proof" || !slices.Equal(evidence.AMR, []string{"pop", "iris"}) {
					t.Fatal("controlled success did not validate the documented Orb methods")
				}
			case simulation.Denied:
				if !errors.Is(err, world.ErrDenied) || evidence.AttemptID != "" || store.completed {
					t.Fatal("denial yielded evidence")
				}
			case simulation.TokenError:
				if !errors.Is(err, world.ErrProvider) || err.Error() != world.ErrProvider.Error() || evidence.AttemptID != "" || store.completed {
					t.Fatal("provider failure yielded evidence or an unsanitized error")
				}
			default:
				if !errors.Is(err, world.ErrToken) || evidence.AttemptID != "" || store.completed {
					t.Fatal("invalid authentication yielded evidence")
				}
			}
			authorization, err := url.Parse(auth.AuthorizationURL)
			if err != nil {
				t.Fatal("invalid local authorization URL")
			}
			assertObservedToken(t, outcome, observer.snapshot(), publicKey, registration.ClientID, authorization.Query().Get("nonce"))
			expected := before
			if outcome != simulation.Denied {
				expected++
			}
			if issuer.Observations().TokenRequests != expected {
				t.Fatal("token observations do not match actual exchanges")
			}
			if _, err := client.Callback(context.Background(), binding, callback.Query()); !errors.Is(err, world.ErrAttemptUnavailable) {
				t.Fatal("simulated callback replay accepted")
			}
			if issuer.Observations().TokenRequests != expected {
				t.Fatal("callback replay performed another token exchange")
			}
		})
	}
	for _, target := range []string{"https://auth.world.org/.well-known/openid-configuration", "https://attacker.example/token"} {
		if _, err := issuer.HTTPClient().Get(target); err == nil {
			t.Fatal("simulation transport accepted an external issuer")
		}
	}
	if _, err := issuer.BrowserURL("https://auth.world.org/authorize"); err == nil {
		t.Fatal("production browser URL rewritten")
	}
	if err := issuer.SetOutcome("production"); err == nil {
		t.Fatal("unsupported outcome accepted")
	}
	before := issuer.Observations().TokenRequests
	response, err := issuer.HTTPClient().Post(registration.Issuer+"/token", "application/x-www-form-urlencoded", strings.NewReader("grant_type=authorization_code"))
	if err != nil {
		t.Fatal("local rejected exchange request failed")
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || issuer.Observations().TokenRequests != before+1 {
		t.Fatal("rejected token POST was not counted")
	}
	response, err = issuer.HTTPClient().Get(registration.Issuer + "/token")
	if err != nil {
		t.Fatal("local unsupported token method request failed")
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || issuer.Observations().TokenRequests != before+1 {
		t.Fatal("non-POST request counted as a token exchange")
	}
}

func TestSimulationConfiguration(t *testing.T) {
	issuer, err := simulation.New("http://127.0.0.1:3104/api/purchases/world/callback")
	if err != nil {
		t.Fatal("exact local proxy callback rejected")
	}
	issuer.Close()
	for _, redirect := range []string{"https://app.example/purchases/world/callback", "http://localhost.attacker.example/purchases/world/callback", "http://localhost/other", "http://localhost/purchases/world/callback?next=external"} {
		if issuer, err := simulation.New(redirect); err == nil {
			issuer.Close()
			t.Fatal("unsafe simulation redirect accepted")
		}
	}
}

func TestScenarioIdentityIsolation(t *testing.T) {
	issuer, err := simulation.New("http://127.0.0.1:8000/purchases/world/callback")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(issuer.Close)
	registration := issuer.Registration()
	authorize := func(accountID string) func() world.Evidence {
		t.Helper()
		store := &memoryStore{rows: make(map[string]*row)}
		client, err := world.New(context.Background(), world.Config{Enabled: true, Environment: world.Sandbox, Issuer: registration.Issuer,
			ClientID: registration.ClientID, ClientSecret: registration.ClientSecret, RedirectURI: registration.RedirectURI}, store, issuer.HTTPClient())
		if err != nil {
			t.Fatal(err)
		}
		binding := world.Binding{AccountID: accountID, OrderID: "synthetic-order", IntentHash: strings.Repeat("a", 64), AllowEnrollment: true}
		auth, err := client.Begin(context.Background(), binding)
		if err != nil {
			t.Fatal(err)
		}
		browserURL, err := issuer.BrowserURL(auth.AuthorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		browser := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := browser.Get(browserURL)
		if err != nil {
			t.Fatal("local browser request failed")
		}
		response.Body.Close()
		if response.StatusCode != http.StatusFound {
			t.Fatal("local issuer did not return a callback")
		}
		callback, err := url.Parse(response.Header.Get("Location"))
		if err != nil {
			t.Fatal("invalid local callback")
		}
		return func() world.Evidence {
			t.Helper()
			before := issuer.Observations().TokenRequests
			evidence, err := client.Callback(context.Background(), binding, callback.Query())
			if err != nil || !evidence.Demo || evidence.Environment != world.Sandbox || !store.completed {
				t.Fatal("real client rejected scenario authentication")
			}
			if _, err := client.Callback(context.Background(), binding, callback.Query()); !errors.Is(err, world.ErrAttemptUnavailable) {
				t.Fatal("scenario callback replay accepted")
			}
			if issuer.Observations().TokenRequests != before+1 {
				t.Fatal("scenario callback did not perform exactly one exchange")
			}
			return evidence
		}
	}
	original := authorize("original-account")
	if err := issuer.SetSubject("local-simulation-scenario-one"); err != nil {
		t.Fatal(err)
	}
	first := authorize("first-account")
	same := authorize("first-account")
	if original().Subject != simulation.Subject {
		t.Fatal("scenario rotation changed an existing code identity")
	}
	firstSubject := first().Subject
	if firstSubject == "" || firstSubject == simulation.Subject || same().Subject != firstSubject {
		t.Fatal("scenario identity was not distinct and stable")
	}
	for _, subject := range []string{"", strings.Repeat("s", 256), "subject\n", "subject other", "subject/other"} {
		if err := issuer.SetSubject(subject); err == nil {
			t.Fatal("invalid synthetic subject accepted")
		}
	}
	if authorize("first-account")().Subject != firstSubject {
		t.Fatal("invalid subject change altered the active scenario")
	}
	before := issuer.Observations().TokenRequests
	if err := issuer.SetSubject("local-simulation-scenario-two"); err != nil {
		t.Fatal(err)
	}
	if issuer.Observations().TokenRequests != before {
		t.Fatal("scenario rotation erased exchange observations")
	}
	secondSubject := authorize("second-account")().Subject
	if secondSubject == "" || secondSubject == firstSubject || secondSubject == simulation.Subject {
		t.Fatal("fresh scenario reused a prior identity")
	}
}

type row struct {
	attempt  world.Attempt
	consumed bool
}
type memoryStore struct {
	mu        sync.Mutex
	rows      map[string]*row
	completed bool
}

func (s *memoryStore) Create(_ context.Context, a world.Attempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[a.StateHash] = &row{attempt: a}
	return nil
}
func (s *memoryStore) Consume(_ context.Context, hash string, b world.Binding, now time.Time) (world.Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[hash]
	if r == nil || r.consumed || r.attempt.Binding != b || !now.Before(r.attempt.ExpiresAt) {
		return world.Attempt{}, world.ErrAttemptUnavailable
	}
	r.consumed = true
	return r.attempt, nil
}
func (s *memoryStore) Complete(_ context.Context, id string, e world.Evidence, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.attempt.ID == id && r.consumed && !s.completed && e.Binding == r.attempt.Binding && now.Before(e.ValidUntil) {
			s.completed = true
			return nil
		}
	}
	return world.ErrAttemptUnavailable
}
func (s *memoryStore) Cancel(context.Context, string, world.Binding, time.Time) error {
	return world.ErrAttemptUnavailable
}
