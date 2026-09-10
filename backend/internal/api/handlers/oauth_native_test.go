package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/agentmesh/backend/internal/models"
)

// Long enough to clear issueToken's own 32-byte floor, so a failure here is
// about the flow rather than about configuration.
const testSecret = "test-jwt-secret-not-for-production-32b"

func testDeps() *Deps {
	return &Deps{JWTSecret: testSecret, FrontendURL: "https://app.test", BaseURL: "https://api.test"}
}

// ── state encoding ──────────────────────────────────────────────────────────
//
// The state string is the only thing carried across the round trip, so what it
// can and cannot say is the whole contract between OAuthStart and
// OAuthCallback.

func TestWebStateIsNotMistakenForNative(t *testing.T) {
	// A bare random hex string is what the web flow has always produced. If one
	// of those ever reads as native, every browser sign-in redirects into an
	// app the browser does not have.
	for _, state := range []string{"", "deadbeef", "0123456789abcdef0123456789abcdef"} {
		if native, _ := decodeNativeState(state); native {
			t.Fatalf("web state %q was read as native", state)
		}
	}
}

func TestNativeStateRoundTrip(t *testing.T) {
	challenge := challengeOf("a-verifier")
	state := encodeNativeState(challenge, "0123456789abcdef")

	native, got := decodeNativeState(state)
	if !native {
		t.Fatal("a state built by encodeNativeState must read as native")
	}
	if got != challenge {
		t.Fatalf("challenge did not survive the round trip: got %q want %q", got, challenge)
	}
}

// A challenge is base64url of a SHA-256 digest, so it can never contain the
// separator. Asserting that is what makes splitting on "." safe.
func TestChallengeCannotContainTheSeparator(t *testing.T) {
	if strings.Contains(challengeOf("anything at all"), ".") {
		t.Fatal("a base64url challenge must not contain the state separator")
	}
}

func TestMalformedNativeStateYieldsNoChallenge(t *testing.T) {
	// Native-looking but truncated. The callback must refuse it rather than
	// fall through to the web path and set cookies for a client that has no use
	// for them.
	native, challenge := decodeNativeState(nativeStatePrefix + "onlychallenge")
	if !native {
		t.Fatal("a state carrying the native prefix must still read as native")
	}
	if challenge != "" {
		t.Fatalf("a malformed native state must yield no challenge, got %q", challenge)
	}
}

// ── who counts as the app ───────────────────────────────────────────────────

func TestIsNativeStart(t *testing.T) {
	cases := map[string]bool{
		"":         false,
		"android":  true,
		"Android":  true,
		"  IOS  ":  true,
		"web":      false,
		"androidx": false,
	}
	for value, want := range cases {
		req := httptest.NewRequest(http.MethodGet,
			"/auth/oauth/google?"+nativeClientParam+"="+url.QueryEscape(value), nil)
		if got := isNativeStart(req); got != want {
			t.Fatalf("client=%q: got %v want %v", value, got, want)
		}
	}
}

// ── the exchange code ───────────────────────────────────────────────────────

func TestExchangeCodeIsRejectedAsASessionToken(t *testing.T) {
	// The security claim the whole design rests on: the code travels on a front
	// channel, so it must be useless as a bearer credential. NewAuthMiddleware
	// refuses any token carrying an iss claim, and this is what puts one there.
	d := testDeps()
	code, err := d.issueExchangeCode("user-1", "a@b.test", challengeOf("v"))
	if err != nil {
		t.Fatalf("issueExchangeCode: %v", err)
	}

	var claims jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(code, &claims); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Issuer != exchangeIssuer {
		t.Fatalf("exchange code must carry iss=%q, got %q", exchangeIssuer, claims.Issuer)
	}
}

func TestSessionTokenCarriesNoIssuer(t *testing.T) {
	// The other half of the same rule: the token the app ends up with must NOT
	// carry an issuer, or the middleware would refuse the very thing it is
	// meant to use.
	d := testDeps()
	session, err := d.issueToken(models.User{ID: "user-1", Email: "a@b.test"})
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	var claims jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(session, &claims); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Issuer != "" {
		t.Fatalf("a session token must carry no issuer, got %q", claims.Issuer)
	}
}

func TestExchangeCodeExpiresQuickly(t *testing.T) {
	d := testDeps()
	code, err := d.issueExchangeCode("user-1", "a@b.test", challengeOf("v"))
	if err != nil {
		t.Fatalf("issueExchangeCode: %v", err)
	}
	var claims jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(code, &claims); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if life := time.Until(claims.ExpiresAt.Time); life > 2*time.Minute {
		t.Fatalf("exchange code lives %v, long enough to be a credential", life)
	}
}

func postExchange(t *testing.T, d *Deps, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/oauth/exchange", strings.NewReader(body))
	rec := httptest.NewRecorder()
	d.OAuthExchange(rec, req)
	return rec
}

func TestExchangeReturnsATokenForTheRightVerifier(t *testing.T) {
	d := testDeps()
	d.Store = oauthTestStore(t)
	verifier := "the-verifier-the-app-kept"
	code, err := d.issueExchangeCode("user-1", "a@b.test", challengeOf(verifier))
	if err != nil {
		t.Fatalf("issueExchangeCode: %v", err)
	}

	rec := postExchange(t, d, `{"code":"`+code+`","verifier":"`+verifier+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Token == "" {
		t.Fatal("no token in the response")
	}
	// A bearer client gets no cookie: setting one would put a session on the
	// API's own domain for a browser that is not the app's.
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("the exchange must set no cookies, got %v", got)
	}
}

func TestExchangeRefusesTheWrongVerifier(t *testing.T) {
	// The point of PKCE here: a code intercepted on the way back is not enough.
	d := testDeps()
	code, err := d.issueExchangeCode("user-1", "a@b.test", challengeOf("the-real-verifier"))
	if err != nil {
		t.Fatalf("issueExchangeCode: %v", err)
	}

	if rec := postExchange(t, d, `{"code":"`+code+`","verifier":"a-guess"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}

func TestExchangeRefusesAnExpiredCode(t *testing.T) {
	d := testDeps()
	verifier := "v"
	claims := exchangeClaims{
		UserID:    "user-1",
		Challenge: challengeOf(verifier),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    exchangeIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Minute)),
		},
	}
	code, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if rec := postExchange(t, d, `{"code":"`+code+`","verifier":"`+verifier+`"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}

func TestExchangeRefusesASessionTokenAsACode(t *testing.T) {
	// A stolen session token must not be laundered into a fresh one through
	// this endpoint. The issuer check is what stops it.
	d := testDeps()
	session, err := d.issueToken(models.User{ID: "user-1", Email: "a@b.test"})
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	if rec := postExchange(t, d, `{"code":"`+session+`","verifier":"anything"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}

func TestExchangeRejectsAnIncompleteBody(t *testing.T) {
	d := testDeps()
	for _, body := range []string{`{}`, `{"code":"x"}`, `{"verifier":"x"}`} {
		if rec := postExchange(t, d, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status %d, want 400", body, rec.Code)
		}
	}
}

// ── OAuthStart's native branch ──────────────────────────────────────────────

func startFor(t *testing.T, d *Deps, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/google"+query, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider", "google")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	d.OAuthStart(rec, req)
	return rec
}

func configuredDeps() *Deps {
	d := testDeps()
	d.GoogleClientID = "id"
	d.GoogleClientSecret = "secret"
	return d
}

// A native start with no challenge is refused back INTO the app, rather than
// onto a web page the app has no way to show.
func TestNativeStartWithoutAChallengeFailsIntoTheApp(t *testing.T) {
	rec := startFor(t, configuredDeps(), "?"+nativeClientParam+"=android")

	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, nativeAppScheme+"://auth?") {
		t.Fatalf("a native failure must redirect into the app, got %q", loc)
	}
	if !strings.Contains(loc, "error=no_challenge") {
		t.Fatalf("the reason must survive, got %q", loc)
	}
}

func TestNativeStartPutsTheChallengeInTheState(t *testing.T) {
	d := configuredDeps()
	challenge := challengeOf("verifier")
	rec := startFor(t, d, "?"+nativeClientParam+"=android&challenge="+url.QueryEscape(challenge))

	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	state := loc.Query().Get("state")
	native, got := decodeNativeState(state)
	if !native || got != challenge {
		t.Fatalf("state %q did not carry the challenge", state)
	}

	// The cookie must hold the same string, or the callback's byte-compare
	// fails and every native sign-in reports invalid_state.
	var cookieValue string
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthStateCookie("google") {
			cookieValue = c.Value
		}
	}
	if cookieValue != state {
		t.Fatalf("cookie %q does not match state %q", cookieValue, state)
	}
}

func TestWebStartIsUnchanged(t *testing.T) {
	d := configuredDeps()
	rec := startFor(t, d, "")

	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if native, _ := decodeNativeState(loc.Query().Get("state")); native {
		t.Fatal("a web start must not produce a native state")
	}
	// The provider redirects to whatever is registered in the Google and GitHub
	// consoles. If this ever changes, sign-in breaks everywhere until someone
	// edits both consoles -- which is exactly what this design avoids.
	if want := d.FrontendURL + "/api/auth/oauth/google/callback"; loc.Query().Get("redirect_uri") != want {
		t.Fatalf("redirect_uri changed: got %q want %q", loc.Query().Get("redirect_uri"), want)
	}
}
