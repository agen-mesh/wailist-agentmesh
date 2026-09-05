package push

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// The product decision, pinned. If someone changes the rule, these are what
// should fail first -- not a device somewhere going quiet.
func TestShouldNotify(t *testing.T) {
	cases := []struct {
		name        string
		triggeredBy string
		status      models.RunStatus
		want        bool
	}{
		// A person pressed Run and is already looking at the result.
		{"manual success stays quiet", "manual", models.RunStatusSuccess, false},
		{"tendril console success stays quiet", "tendril-console", models.RunStatusSuccess, false},

		// Nobody is watching. This is what push is for.
		{"geofence success notifies", "geofence", models.RunStatusSuccess, true},
		{"schedule success notifies", "schedule", models.RunStatusSuccess, true},
		{"webhook success notifies", "webhook", models.RunStatusSuccess, true},

		// A failure is worth hearing about however it started: the thing you
		// were waiting for is not coming.
		{"manual failure still notifies", "manual", models.RunStatusFailed, true},
		{"geofence failure notifies", "geofence", models.RunStatusFailed, true},

		// Not terminal, so there is nothing to report yet.
		{"running notifies nothing", "geofence", models.RunStatusRunning, false},
		// Somebody pressed stop. They know.
		{"stopped notifies nothing", "geofence", models.RunStatusStopped, false},
		{"stopped manual notifies nothing", "manual", models.RunStatusStopped, false},

		// An unrecognised trigger counts as automatic: a new trigger is far
		// likelier to be another machine source than another button.
		{"unknown trigger counts as automatic", "carrier-pigeon", models.RunStatusSuccess, true},
		{"empty trigger counts as automatic", "", models.RunStatusSuccess, true},

		// triggered_by is free text written at four call sites; nothing
		// normalises it on the way in.
		{"case and spacing do not change the answer", "  Manual  ", models.RunStatusSuccess, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldNotify(c.triggeredBy, c.status); got != c.want {
				t.Fatalf("ShouldNotify(%q, %q) = %v, want %v", c.triggeredBy, c.status, got, c.want)
			}
		})
	}
}

func TestRunFinished(t *testing.T) {
	t.Run("carries the workflow name and the outcome", func(t *testing.T) {
		n := RunFinished("wf_1", "Morning briefing", "run_1", models.RunStatusSuccess)
		if n.Title != "Morning briefing" {
			t.Fatalf("title = %q", n.Title)
		}
		if !strings.Contains(n.Body, "finished") {
			t.Fatalf("body = %q, want it to say the run finished", n.Body)
		}
		// The run id is what makes tapping the notification open that run
		// rather than the app's front door.
		if n.Data["runId"] != "run_1" {
			t.Fatalf("data runId = %q", n.Data["runId"])
		}
		if n.Data["status"] != "success" {
			t.Fatalf("data status = %q", n.Data["status"])
		}
		// Without the workflow id a tap has nowhere to navigate: the run view
		// only exists inside that workflow's page.
		if n.Data["workflowId"] != "wf_1" {
			t.Fatalf("data workflowId = %q", n.Data["workflowId"])
		}
	})

	t.Run("says failed when it failed", func(t *testing.T) {
		n := RunFinished("wf_1", "Morning briefing", "run_1", models.RunStatusFailed)
		if !strings.Contains(n.Body, "failed") {
			t.Fatalf("body = %q, want it to say the run failed", n.Body)
		}
	})

	t.Run("does not render an empty title", func(t *testing.T) {
		// A blank title looks like a bug on a lock screen, where there is no
		// surrounding context to fill the gap.
		n := RunFinished("wf_1", "   ", "run_1", models.RunStatusSuccess)
		if strings.TrimSpace(n.Title) == "" {
			t.Fatal("title is blank")
		}
	})
}

func TestParseCredentials(t *testing.T) {
	t.Run("an unset variable is not an error worth logging", func(t *testing.T) {
		// This is the state the repo ships in until somebody creates a Firebase
		// project, so it has to stay distinguishable from a real fault.
		if _, err := parseCredentials(""); err != errNotConfigured {
			t.Fatalf("err = %v, want errNotConfigured", err)
		}
		if _, err := parseCredentials("   \n "); err != errNotConfigured {
			t.Fatalf("whitespace-only: err = %v, want errNotConfigured", err)
		}
	})

	t.Run("malformed JSON does not leak the input", func(t *testing.T) {
		// The input is a private key. An error quoting what it failed to parse
		// would put that key in the log.
		secret := `{"private_key": "-----BEGIN PRIVATE KEY-----SUPERSECRET`
		_, err := parseCredentials(secret)
		if err == nil {
			t.Fatal("want an error")
		}
		if strings.Contains(err.Error(), "SUPERSECRET") {
			t.Fatalf("error message leaked key material: %v", err)
		}
	})

	t.Run("rejects a document missing what it needs", func(t *testing.T) {
		if _, err := parseCredentials(`{"project_id":"p"}`); err == nil {
			t.Fatal("want an error for a service account with no key")
		}
	})

	t.Run("defaults the token endpoint", func(t *testing.T) {
		c, err := parseCredentials(`{"project_id":"p","client_email":"e","private_key":"k"}`)
		if err != nil {
			t.Fatal(err)
		}
		if c.TokenURI == "" {
			t.Fatal("token_uri was left empty")
		}
	})
}

func TestSignedJWT(t *testing.T) {
	_, pemKey := testKey(t)
	c := &credentials{
		ProjectID:   "proj",
		ClientEmail: "sa@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    "https://oauth2.googleapis.com/token",
	}

	assertion, err := c.signedJWT(time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("assertion has %d parts, want 3", len(parts))
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != c.ClientEmail {
		t.Fatalf("iss = %v", claims["iss"])
	}
	if claims["scope"] != fcmScope {
		t.Fatalf("scope = %v", claims["scope"])
	}
	// Google rejects an assertion valid for longer than an hour.
	iat, exp := claims["iat"].(float64), claims["exp"].(float64)
	if exp-iat > 3600 {
		t.Fatalf("assertion valid for %v seconds, more than the hour Google allows", exp-iat)
	}
}

func TestParsePrivateKey(t *testing.T) {
	_, pemKey := testKey(t)

	t.Run("reads a normal PEM", func(t *testing.T) {
		if _, err := parsePrivateKey(pemKey); err != nil {
			t.Fatal(err)
		}
	})

	// The single most common way a correct service account fails to work: the
	// key is pasted into an environment variable with its newlines still
	// escaped, so it is valid JSON but not valid PEM.
	t.Run("reads a PEM whose newlines are still escaped", func(t *testing.T) {
		escaped := strings.ReplaceAll(pemKey, "\n", "\\n")
		if _, err := parsePrivateKey(escaped); err != nil {
			t.Fatalf("escaped newlines should be tolerated: %v", err)
		}
	})

	t.Run("rejects something that is not a key", func(t *testing.T) {
		if _, err := parsePrivateKey("not a pem"); err == nil {
			t.Fatal("want an error")
		}
	})
}

// -- Delivery ---------------------------------------------------------------

type fakeStore struct {
	mu      sync.Mutex
	tokens  []models.DeviceToken
	deleted []string
}

func (f *fakeStore) DeviceTokensForUser(context.Context, string) ([]models.DeviceToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens, nil
}

func (f *fakeStore) DeleteDeviceTokenByValue(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, token)
	return nil
}

// Points the package at a fake FCM and a fake token endpoint for one test,
// restoring the real ones afterwards. errorCode, when non-empty, is sent
// back as error.details[].errorCode in FCM's real HTTP v1 shape -- send()
// reads that field, not the bare HTTP status, to decide whether a token is
// dead.
func withFakeFCM(t *testing.T, fcmStatus int, errorCode string) *fakeStore {
	t.Helper()
	_, pemKey := testKey(t)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at_test","expires_in":3600}`))
	}))
	t.Cleanup(tokenSrv.Close)

	fcmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(fcmStatus)
		if fcmStatus < 300 || errorCode == "" {
			return
		}
		_, _ = fmt.Fprintf(w, `{"error":{"status":"UNKNOWN","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":%q}]}}`, errorCode)
	}))
	t.Cleanup(fcmSrv.Close)

	oldBase := fcmBaseURL
	fcmBaseURL = fcmSrv.URL
	t.Cleanup(func() { fcmBaseURL = oldBase })

	// loadCredentials caches behind a sync.Once, so the test sets the parsed
	// value directly rather than fighting it through the environment.
	oldVal, oldErr := credsVal, credsErr
	credsOnce.Do(func() {})
	credsVal = &credentials{
		ProjectID:   "proj",
		ClientEmail: "sa@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    tokenSrv.URL,
	}
	credsErr = nil
	t.Cleanup(func() { credsVal, credsErr = oldVal, oldErr })

	// The access token is cached across calls; clear it so each test mints one.
	tokenMu.Lock()
	cachedToken, cachedExpiry = "", time.Time{}
	tokenMu.Unlock()

	return &fakeStore{tokens: []models.DeviceToken{{Token: "tok_live", UserID: "u1"}}}
}

func TestDeliver(t *testing.T) {
	t.Run("a rejected token is dropped", func(t *testing.T) {
		// 404 with errorCode UNREGISTERED: the app was uninstalled or the
		// token rotated. Keeping the row would mean retrying a guaranteed
		// failure forever.
		store := withFakeFCM(t, http.StatusNotFound, "UNREGISTERED")
		Deliver(context.Background(), store, "u1", Notification{Title: "t", Body: "b"})
		if len(store.deleted) != 1 || store.deleted[0] != "tok_live" {
			t.Fatalf("deleted = %v, want [tok_live]", store.deleted)
		}
	})

	// The regression this guards: an earlier version treated EVERY 400 as a
	// dead token. INVALID_ARGUMENT covers a malformed request field at least
	// as often as a malformed token -- a payload bug on our side must not
	// delete a live registration, let alone every registration a run's send
	// loop touches.
	t.Run("a malformed-request response keeps the token", func(t *testing.T) {
		store := withFakeFCM(t, http.StatusBadRequest, "INVALID_ARGUMENT")
		Deliver(context.Background(), store, "u1", Notification{Title: "t", Body: "b"})
		if len(store.deleted) != 0 {
			t.Fatalf("deleted = %v, want nothing dropped", store.deleted)
		}
	})

	t.Run("a transient failure keeps the token", func(t *testing.T) {
		// 503 is FCM having a bad day, not a verdict on the token. Dropping it
		// would cost the user their registration over somebody else's outage.
		store := withFakeFCM(t, http.StatusServiceUnavailable, "UNAVAILABLE")
		Deliver(context.Background(), store, "u1", Notification{Title: "t", Body: "b"})
		if len(store.deleted) != 0 {
			t.Fatalf("deleted = %v, want nothing dropped", store.deleted)
		}
	})

	t.Run("a rate limit keeps the token", func(t *testing.T) {
		store := withFakeFCM(t, http.StatusTooManyRequests, "QUOTA_EXCEEDED")
		Deliver(context.Background(), store, "u1", Notification{Title: "t", Body: "b"})
		if len(store.deleted) != 0 {
			t.Fatalf("deleted = %v, want nothing dropped", store.deleted)
		}
	})

	t.Run("a success drops nothing", func(t *testing.T) {
		store := withFakeFCM(t, http.StatusOK, "")
		Deliver(context.Background(), store, "u1", Notification{Title: "t", Body: "b"})
		if len(store.deleted) != 0 {
			t.Fatalf("deleted = %v, want nothing dropped", store.deleted)
		}
	})

	t.Run("no devices means nothing is sent", func(t *testing.T) {
		store := withFakeFCM(t, http.StatusOK, "")
		store.tokens = nil
		// Must not panic and must not reach the network.
		Deliver(context.Background(), store, "u1", Notification{Title: "t", Body: "b"})
	})
}

func TestNotifyRunFinishedRespectsTheRule(t *testing.T) {
	store := withFakeFCM(t, http.StatusNotFound, "UNREGISTERED")
	// A manual success must not send at all, so the 404 above should never be
	// reached and nothing should be dropped.
	NotifyRunFinished(context.Background(), store, "u1", "wf_1", "wf", "run_1", "manual", models.RunStatusSuccess)
	if len(store.deleted) != 0 {
		t.Fatalf("a manual success reached FCM; deleted = %v", store.deleted)
	}
}

// fcmErrorCode reads the field that actually distinguishes a dead token from
// a malformed request -- error.details[].errorCode -- and falls back to
// error.status when a response carries no details at all.
func TestFcmErrorCode(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"UNREGISTERED in details",
			`{"error":{"status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`,
			"UNREGISTERED",
		},
		{
			"INVALID_ARGUMENT in details, distinct from UNREGISTERED",
			`{"error":{"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"INVALID_ARGUMENT"}]}}`,
			"INVALID_ARGUMENT",
		},
		{
			"falls back to top-level status with no details",
			`{"error":{"status":"UNAVAILABLE"}}`,
			"UNAVAILABLE",
		},
		{"empty body", ``, ""},
		{"not JSON at all", `<html>502 Bad Gateway</html>`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fcmErrorCode([]byte(c.body)); got != c.want {
				t.Fatalf("fcmErrorCode(%q) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	// 1024 is not a size to ship, but this key exists for the length of one
	// test and 2048 measurably slows the package down.
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}
