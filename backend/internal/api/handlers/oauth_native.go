package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// OAuth sign-in for the native Android shell.
//
// The web flow ends by setting an HttpOnly cookie on the API's domain and
// redirecting to FRONTEND_URL. Neither half of that reaches the app: the
// WebView's origin is https://localhost, so the cookie is third-party and
// Android declines it, and there is no page on that origin for the browser to
// land on anyway. The app authenticates by bearer token (see wantsBearerToken)
// and has no way to obtain one through a redirect chain it cannot observe.
//
// So the native flow diverges only at the very end:
//
//	app                      browser (Custom Tab)             backend
//	 | verifier := random     |                                 |
//	 | challenge := sha256    |                                 |
//	 |--- open /auth/oauth/google?client=android&challenge= --->|
//	 |                        |<-- 302 to provider, state ------|
//	 |                        |--- consent, provider redirect ->|
//	 |                        |<-- 302 ai.agentmesh.app://auth?code=
//	 |<-- Android VIEW intent |                                 |
//	 |--- POST /auth/oauth/exchange {code, verifier} ---------->|
//	 |<-- {"token": "<session jwt>"} ---------------------------|
//
// What this deliberately does NOT change is oauthRedirectURI. The provider
// redirects to the URL registered in the Google and GitHub consoles, and that
// stays FRONTEND_URL + /api/auth/oauth/<provider>/callback for both clients.
// Sending the provider somewhere else would mean a console edit per
// environment, and would break the oauth_state_* cookie's origin match for the
// reason oauthRedirectURI's own comment gives. The whole round trip happens in
// the system browser on the frontend's origin exactly as it does for the web;
// only the last hop differs.

// nativeAppScheme is where a native flow hands back. It is the app's
// applicationId, which is also mobile/android/.../values/strings.xml's
// custom_url_scheme and the <data android:scheme> of the VIEW intent-filter.
// A constant rather than configuration: it identifies the app, not the
// deployment, and every environment's app answers to the same one. An
// allowlist read from the environment would be one more way to get it wrong.
const nativeAppScheme = "ai.agentmesh.app"

// nativeClientParam marks a flow as belonging to the app. It has to be a query
// parameter rather than the X-AgentMesh-Client header the rest of the API uses:
// OAuthStart is reached by a top-level browser navigation, and no custom header
// rides along on one.
const nativeClientParam = "client"

// exchangeIssuer makes the code handed to the app useless as a session.
//
// NewAuthMiddleware rejects any token carrying an iss claim outright — a guard
// added for the connector state JWT, which is signed with the same secret and
// travels on a front channel. This reuses it rather than inventing a second
// rule: even though the exchange code is minted by us and lives for a minute,
// presenting it as a bearer token fails at the same line that stops the
// connector one.
const exchangeIssuer = "agentmesh-oauth-exchange"

// exchangeTTL is the window between the provider's redirect and the app's
// exchange call — an Android intent delivery and one HTTPS round trip. A minute
// is generous for that, and short enough that a code left in the device's
// browser history is not a credential by the time anyone reads it.
const exchangeTTL = 60 * time.Second

// nativeStatePrefix marks the state string as belonging to a native flow, and
// carries the PKCE challenge with it.
//
// Encoded INTO the state rather than stored anywhere. State is already written
// to the oauth_state_<provider> cookie and compared byte-for-byte on the way
// back, so anything inside it is carried across the round trip and made
// tamper-evident for free — a second cookie or a server-side table would add
// storage for a value the existing one already protects. The shape is
//
//	m.<challenge>.<random>
//
// and a web flow stays a bare random string, so nothing about the existing path
// changes.
const nativeStatePrefix = "m."

// encodeNativeState builds the state for a native flow. The random tail is what
// actually defends the redirect; the challenge rides along.
func encodeNativeState(challenge, random string) string {
	return nativeStatePrefix + challenge + "." + random
}

// decodeNativeState reports whether a state came from the app and, if so, the
// PKCE challenge it was started with. A malformed native-looking state returns
// native=true with an empty challenge, which the callback then refuses — better
// than falling through to the web path and setting cookies for a client that
// has no use for them.
func decodeNativeState(state string) (native bool, challenge string) {
	if !strings.HasPrefix(state, nativeStatePrefix) {
		return false, ""
	}
	challenge, _, found := strings.Cut(strings.TrimPrefix(state, nativeStatePrefix), ".")
	if !found {
		return true, ""
	}
	return true, challenge
}

// isNativeStart reports whether OAuthStart was called by the app.
func isNativeStart(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(nativeClientParam))) {
	case "android", "ios":
		return true
	}
	return false
}

// challengeOf is the PKCE S256 transform. The app keeps the verifier and sends
// only this, so a code intercepted on the way back cannot be redeemed by
// whoever intercepted it.
func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type exchangeClaims struct {
	UserID    string `json:"sub"`
	Email     string `json:"email"`
	Challenge string `json:"cha"`
	jwt.RegisteredClaims
}

// issueExchangeCode mints the one-time code the app swaps for a session.
//
// A unique identifier keeps separate grants distinct even within one second.
// Redemption is recorded atomically in the shared database.
func (d *Deps) issueExchangeCode(userID, email, challenge string) (string, error) {
	if len(d.JWTSecret) < 32 {
		return "", errors.New("jwt secret not configured")
	}
	id, err := randHex(16)
	if err != nil {
		return "", err
	}
	claims := exchangeClaims{
		UserID:    userID,
		Email:     email,
		Challenge: challenge,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        id,
			Issuer:    exchangeIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(exchangeTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(d.JWTSecret))
}

// nativeRedirect sends the browser back into the app.
//
// url.Values rather than string concatenation because a failure reason is not
// ours to trust as URL-safe, and a malformed deep link fails silently: Android
// simply does not match the intent-filter, the Custom Tab sits there, and
// nothing says why.
func nativeRedirect(w http.ResponseWriter, r *http.Request, q url.Values) {
	http.Redirect(w, r, nativeAppScheme+"://auth?"+q.Encode(), http.StatusFound)
}

// nativeFail is redirectFail's counterpart for a flow that started in the app.
// The app shows the reason on the sign-in screen, the same way the web does
// with /signin?error=.
func nativeFail(w http.ResponseWriter, r *http.Request, reason string) {
	q := url.Values{}
	q.Set("error", reason)
	nativeRedirect(w, r, q)
}

// OAuthExchange trades a one-time code for a session token.
//
// POST rather than GET, and the verifier in the body rather than the query, so
// neither value lands in an access log. The response mirrors SignIn's native
// shape ({"token": ...}) so the client has one thing to parse.
func (d *Deps) OAuthExchange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Code == "" || body.Verifier == "" {
		respond.Error(w, http.StatusBadRequest, "code and verifier are required")
		return
	}

	// Every failure below is the same 401 with the same message. The caller is
	// the app, which can do nothing differently for an expired code than for a
	// forged one, and a more specific error would only tell an attacker which
	// half of the pair they had got right.
	var claims exchangeClaims
	_, err := jwt.ParseWithClaims(body.Code, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(d.JWTSecret), nil
	}, jwt.WithIssuer(exchangeIssuer), jwt.WithExpirationRequired(), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || claims.UserID == "" || claims.ID == "" {
		respond.Error(w, http.StatusUnauthorized, "invalid or expired code")
		return
	}

	// Constant-time, because this is a secret comparison on a public endpoint.
	want := challengeOf(body.Verifier)
	if subtle.ConstantTimeCompare([]byte(want), []byte(claims.Challenge)) != 1 {
		respond.Error(w, http.StatusUnauthorized, "invalid or expired code")
		return
	}

	token, err := d.issueToken(models.User{ID: claims.UserID, Email: claims.Email})
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	if d.Store == nil {
		respond.Error(w, http.StatusServiceUnavailable, "could not exchange code")
		return
	}
	consumed, err := d.Store.ConsumeOAuthExchangeCode(r.Context(), claims.ID, claims.ExpiresAt.Time)
	if err != nil {
		slog.Error("could not consume oauth exchange code", "error", err)
		respond.Error(w, http.StatusServiceUnavailable, "could not exchange code")
		return
	}
	if !consumed {
		respond.Error(w, http.StatusUnauthorized, "invalid or expired code")
		return
	}
	// No cookie. The app is a bearer client, and setting one here would put a
	// session on the API's own domain for a browser that is not the app's.
	respond.JSON(w, http.StatusOK, map[string]any{"token": token})
}
