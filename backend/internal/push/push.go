// Package push delivers run-status notifications to registered devices via
// Firebase Cloud Messaging.
//
// Optional infrastructure, exactly like package alert: with no credentials
// configured every entry point here is a no-op, and a slow or unreachable FCM
// must never block or fail the run it is reporting on. That is deliberate and
// load-bearing -- this code ships and lies dormant until somebody creates a
// Firebase project and supplies FCM_SERVICE_ACCOUNT_JSON.
package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// The service account, as downloaded from the Firebase console. A real secret,
// unlike google-services.json -- note the deliberate absence anywhere below of
// a log line that formats the whole struct.
const serviceAccountEnvVar = "FCM_SERVICE_ACCOUNT_JSON"

// Scope for FCM HTTP v1. The legacy "server key" API this replaces has been
// retired by Google; any guide still describing one is out of date.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

var httpClient = &http.Client{Timeout: 10 * time.Second}

// TokenStore is the slice of the database this package needs, named as an
// interface so the send path can be tested without a Postgres.
type TokenStore interface {
	DeviceTokensForUser(ctx context.Context, userID string) ([]models.DeviceToken, error)
	DeleteDeviceTokenByValue(ctx context.Context, token string) error
}

// ShouldNotify decides whether a finished run is worth waking a phone for.
//
// The rule, following #125's reasoning:
//
//   - A run the user started by hand needs no push. They are looking at the
//     screen that already shows the result; a notification about it is noise,
//     and noise is how an app's notifications get turned off for good.
//   - A run something else started -- a geofence crossing, a schedule, an
//     inbound webhook -- is exactly what a push is for. Nobody is watching,
//     which is the whole point of an automatic trigger.
//   - A FAILURE notifies either way. A run that broke is worth hearing about
//     even if you started it, because the thing you were waiting for is not
//     coming.
//
// Pure and table-tested rather than inlined at the call site: this is a product
// decision, and one that could only be verified by running the whole engine is
// a decision nobody will revisit safely.
func ShouldNotify(triggeredBy string, status models.RunStatus) bool {
	// Anything not finished has nothing to report yet. Stopped runs are
	// excluded on purpose: somebody pressed stop, so they already know.
	if status != models.RunStatusSuccess && status != models.RunStatusFailed {
		return false
	}
	if status == models.RunStatusFailed {
		return true
	}
	return isAutomatic(triggeredBy)
}

// The trigger vocabulary is free text in runs.triggered_by, written at four
// call sites: "manual" and "tendril-console" (a person, watching), "geofence",
// "schedule" and "webhook" (not a person, not watching).
//
// Unknown values count as automatic. A trigger added later is far likelier to
// be another machine source than another kind of button, and the failure modes
// are asymmetric: a missing notification is invisible, a spurious one is merely
// annoying.
func isAutomatic(triggeredBy string) bool {
	switch strings.ToLower(strings.TrimSpace(triggeredBy)) {
	case "manual", "tendril-console":
		return false
	default:
		return true
	}
}

// Notification is what a device is asked to display.
type Notification struct {
	Title string
	Body  string
	// Data rides alongside for the app to act on when the notification is
	// tapped -- the run id, so it opens that run rather than the app's front
	// door.
	Data map[string]string
}

// RunFinished builds the notification for a completed run.
//
// Separate from delivery so the wording can be tested without a network, and so
// it lives in one place rather than being assembled inline inside the engine.
func RunFinished(workflowID, workflowName, runID string, status models.RunStatus) Notification {
	outcome := "finished"
	if status == models.RunStatusFailed {
		outcome = "failed"
	}
	if strings.TrimSpace(workflowName) == "" {
		workflowName = "A workflow"
	}
	return Notification{
		Title: workflowName,
		Body:  fmt.Sprintf("Run %s.", outcome),
		Data: map[string]string{
			"runId": runID,
			// The workflow id, not only the run id: the app's only view of a
			// run lives inside that workflow's page, so a tap with just a run
			// id has nowhere to go.
			"workflowId": workflowID,
			"status":     string(status),
		},
	}
}

// NotifyRunFinished sends a run's outcome to every device the owner has
// registered, if the rule above says it is worth sending.
//
// Call it as `go push.NotifyRunFinished(...)`, the way alert.Notify is called:
// FCM is somebody else's server and must never sit on the critical path of
// finishing a run.
func NotifyRunFinished(ctx context.Context, store TokenStore, userID, workflowID, workflowName, runID, triggeredBy string, status models.RunStatus) {
	if !ShouldNotify(triggeredBy, status) {
		return
	}
	Deliver(ctx, store, userID, RunFinished(workflowID, workflowName, runID, status))
}

// Deliver sends one notification to every device belonging to userID.
//
// Errors are logged, never returned. No caller is in a position to do anything
// useful with a push failure, and one that failed a run over it would be
// trading a working workflow for a missing notification.
func Deliver(ctx context.Context, store TokenStore, userID string, n Notification) {
	creds, err := loadCredentials()
	if err != nil {
		// The unconfigured case is the expected one until a Firebase project
		// exists, and it is silent by design -- logging on every finished run
		// would bury the log under a feature nobody has switched on.
		if !errors.Is(err, errNotConfigured) {
			log.Printf("push: credentials unusable: %v", err)
		}
		return
	}

	tokens, err := store.DeviceTokensForUser(ctx, userID)
	if err != nil {
		log.Printf("push: could not list devices: %v", err)
		return
	}
	if len(tokens) == 0 {
		return
	}

	accessToken, err := creds.accessToken(ctx)
	if err != nil {
		log.Printf("push: could not mint an access token: %v", err)
		return
	}

	// Sequential and uncapped. One person's devices is a handful, and this
	// already runs on its own goroutine off finishRun, so the latency costs
	// nobody anything today.
	//
	// TODO(#132): cap this if the shape ever changes -- a per-user device list
	// long enough to matter, or a caller that fans out to many users at once,
	// would turn one finished run into an unbounded burst of serial HTTPS
	// requests. Store.DeviceTokensForUser's comment already anticipates a cap;
	// this is the place that would need one.
	for _, d := range tokens {
		dead, err := send(ctx, creds.ProjectID, accessToken, d.Token, n)
		if err != nil {
			log.Printf("push: send failed: %v", err)
		}
		if dead {
			// FCM says this token no longer addresses anything -- the app was
			// uninstalled, or the token was rotated. Dropping the row is the
			// only thing that stops the table growing a permanent tail of
			// addresses that fail on every future run.
			if err := store.DeleteDeviceTokenByValue(ctx, d.Token); err != nil {
				log.Printf("push: could not drop a dead token: %v", err)
			}
		}
	}
}

// send posts one message. The bool reports whether FCM considers the token
// permanently dead, which is a different question from whether the send
// failed: a timeout is worth retrying next time, an unregistered token never
// is. Decided from FCM's own error code in the response body (see
// fcmErrorCode), not from the HTTP status alone -- a 400 covers a malformed
// request as often as a malformed token.
func send(ctx context.Context, projectID, accessToken, deviceToken string, n Notification) (dead bool, err error) {
	body, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"notification": map[string]string{
				"title": n.Title,
				"body":  n.Body,
			},
			"data": n.Data,
			"android": map[string]any{
				// A run result is worth showing promptly but is not an alarm.
				// "high" would be a claim on the user's attention that a
				// finished workflow has not earned, and Android increasingly
				// penalises apps that overstate it.
				"priority": "normal",
			},
		},
	})
	if err != nil {
		return false, fmt.Errorf("marshal: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1/projects/%s/messages:send", fcmBaseURL, url.PathEscape(projectID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	// FCM's own verdict, not the HTTP status, decides whether a token is
	// dead. A 400 is INVALID_ARGUMENT by Google's error table, and that
	// covers a malformed request field -- our bug -- at least as often as a
	// malformed token; only an errorCode of UNREGISTERED (which Google
	// returns as a 404) means the app instance is gone and the token will
	// never work again. Treating every 400 as dead, as an earlier version of
	// this did, would delete a live registration on every account the first
	// time a payload bug shipped -- a fleet-wide loss over a bug that had
	// nothing to do with any one token.
	code := fcmErrorCode(respBody)
	dead = code == "UNREGISTERED"
	return dead, fmt.Errorf("fcm returned %d (%s): %s", resp.StatusCode, code, truncate(string(respBody), 300))
}

// fcmErrorCode extracts FCM HTTP v1's error code from a failed response body.
//
// The code that actually distinguishes "this token is gone" from "this
// request was malformed" lives in error.details[].errorCode (an
// FcmError-typed detail), not in the top-level HTTP status or error.status --
// the shape documented at
// https://firebase.google.com/docs/reference/fcm/rest/v1/ErrorCode. Falls
// back to error.status when Google ever sends a response with no details, so
// a shape this package has not seen still surfaces something rather than an
// empty string.
func fcmErrorCode(body []byte) string {
	var parsed struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return ""
	}
	for _, d := range parsed.Error.Details {
		if d.ErrorCode != "" {
			return d.ErrorCode
		}
	}
	return parsed.Error.Status
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Overridden in tests to point at an httptest server. A var rather than a
// constant for that reason alone; nothing in production reassigns it.
var fcmBaseURL = "https://fcm.googleapis.com"

// -- Service account credentials ---------------------------------------------

var errNotConfigured = errors.New("push: no service account configured")

type credentials struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// Parsed once and cached: the env var does not change while the process runs,
// and re-parsing an RSA key on every finished run is pure waste.
var (
	credsOnce sync.Once
	credsVal  *credentials
	credsErr  error
)

func loadCredentials() (*credentials, error) {
	credsOnce.Do(func() { credsVal, credsErr = parseCredentials(os.Getenv(serviceAccountEnvVar)) })
	return credsVal, credsErr
}

// Split from loadCredentials so it can be tested without touching the process
// environment or defeating the sync.Once.
func parseCredentials(raw string) (*credentials, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errNotConfigured
	}
	var c credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		// Deliberately drops the underlying error and the input: a malformed
		// service account is still a service account, and the private key must
		// not reach the log by way of a parse error quoting it.
		return nil, errors.New("service account JSON could not be parsed")
	}
	if c.ProjectID == "" || c.ClientEmail == "" || c.PrivateKey == "" {
		return nil, errors.New("service account JSON is missing project_id, client_email or private_key")
	}
	if c.TokenURI == "" {
		c.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &c, nil
}

// Access tokens last an hour; minting one per notification would put a round
// trip to Google in front of every send.
var (
	tokenMu      sync.Mutex
	cachedToken  string
	cachedExpiry time.Time
)

func (c *credentials) accessToken(ctx context.Context) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	// A minute of headroom, so a token about to expire is not handed to a
	// request that will still be in flight when it does.
	if cachedToken != "" && time.Now().Before(cachedExpiry.Add(-time.Minute)) {
		return cachedToken, nil
	}

	assertion, err := c.signedJWT(time.Now())
	if err != nil {
		return "", err
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("token endpoint returned no access_token")
	}

	cachedToken = out.AccessToken
	cachedExpiry = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return cachedToken, nil
}

// signedJWT builds the RS256 assertion Google exchanges for an access token.
//
// Hand-rolled rather than pulling in golang.org/x/oauth2/google. It is a
// header, a claim set and one signature -- around forty lines against a
// dependency tree this repo otherwise has no use for, and the project already
// prefers writing the small thing (the geofence plugin exists instead of a
// background-tracking SDK for the same reason).
func (c *credentials) signedJWT(now time.Time) (string, error) {
	key, err := parsePrivateKey(c.PrivateKey)
	if err != nil {
		return "", err
	}

	header := base64URL([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iss":   c.ClientEmail,
		"scope": fcmScope,
		"aud":   c.TokenURI,
		"iat":   now.Unix(),
		// One hour is the maximum Google accepts.
		"exp": now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}

	signingInput := header + "." + base64URL(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64URL(sig), nil
}

func parsePrivateKey(pemKey string) (*rsa.PrivateKey, error) {
	// The JSON carries the PEM with literal \n escapes, which json.Unmarshal
	// has already turned into real newlines. A key pasted into an environment
	// variable by hand often has not, so both are accepted -- this is the
	// single most common way a correct service account fails to work.
	pemKey = strings.ReplaceAll(pemKey, "\\n", "\n")

	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("private_key is not valid PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private_key is not an RSA key")
		}
		return rsaKey, nil
	}
	// Google issues PKCS#8, but a key round-tripped through other tooling can
	// come back as PKCS#1.
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
