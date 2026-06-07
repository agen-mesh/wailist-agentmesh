package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/respond"
)

type oauthProvider struct {
	authURL      string
	tokenURL     string
	userInfoURL  string
	scope        string
	clientID     string
	clientSecret string
}

func (d *Deps) providerConfig(name string) (oauthProvider, bool) {
	switch name {
	case "github":
		return oauthProvider{
			authURL:      "https://github.com/login/oauth/authorize",
			tokenURL:     "https://github.com/login/oauth/access_token",
			userInfoURL:  "https://api.github.com/user",
			scope:        "read:user user:email",
			clientID:     d.GithubClientID,
			clientSecret: d.GithubClientSecret,
		}, d.GithubClientID != "" && d.GithubClientSecret != ""
	case "google":
		return oauthProvider{
			authURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			tokenURL:     "https://oauth2.googleapis.com/token",
			userInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
			scope:        "openid email profile",
			clientID:     d.GoogleClientID,
			clientSecret: d.GoogleClientSecret,
		}, d.GoogleClientID != "" && d.GoogleClientSecret != ""
	}
	return oauthProvider{}, false
}

// OAuthStart redirects the browser to the provider's consent screen.
func (d *Deps) OAuthStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "provider")
	p, ok := d.providerConfig(name)
	if !ok {
		respond.Error(w, http.StatusNotFound, "unknown or unconfigured provider")
		return
	}

	state, err := randHex(16)
	if err != nil {
		d.redirectFail(w, r, "internal")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie(name),
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	q := url.Values{}
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", d.oauthRedirectURI(name))
	q.Set("scope", p.scope)
	q.Set("state", state)
	q.Set("response_type", "code")
	http.Redirect(w, r, p.authURL+"?"+q.Encode(), http.StatusFound)
}

// OAuthCallback handles the provider redirect: verifies state, exchanges the
// code, fetches the email, upserts the user, issues a JWT, and bounces the
// browser back to the frontend with the token.
func (d *Deps) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "provider")
	p, ok := d.providerConfig(name)
	if !ok {
		respond.Error(w, http.StatusNotFound, "unknown or unconfigured provider")
		return
	}

	cookieName := oauthStateCookie(name)
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" || cookie.Value != r.URL.Query().Get("state") {
		d.redirectFail(w, r, "invalid_state")
		return
	}
	// One-time use: clear the state cookie so it cannot be replayed.
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})

	code := r.URL.Query().Get("code")
	if code == "" {
		d.redirectFail(w, r, "no_code")
		return
	}

	accessToken, err := exchangeCode(p, code, d.oauthRedirectURI(name))
	if err != nil {
		d.redirectFail(w, r, "token_exchange")
		return
	}

	email, err := fetchEmail(name, p, accessToken)
	if err != nil || email == "" {
		d.redirectFail(w, r, "no_email")
		return
	}

	user, err := d.Store.GetOrCreateOAuthUser(r.Context(), strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, db.ErrPasswordAccountExists) {
		d.redirectFail(w, r, "account_exists")
		return
	}
	if err != nil {
		d.redirectFail(w, r, "user_upsert")
		return
	}

	token, err := d.issueToken(user)
	if err != nil {
		d.redirectFail(w, r, "token_issue")
		return
	}

	// Token is delivered in the URL fragment, not the query string: fragments are
	// never sent to the server (no access logs, no Referer leak). The callback page
	// reads it and immediately clears it from history via replaceState.
	http.Redirect(w, r, d.FrontendURL+"/auth/callback#token="+url.QueryEscape(token), http.StatusFound)
}

func (d *Deps) oauthRedirectURI(provider string) string {
	return strings.TrimRight(d.BaseURL, "/") + "/auth/oauth/" + provider + "/callback"
}

func (d *Deps) redirectFail(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, d.FrontendURL+"/signin?error="+url.QueryEscape(reason), http.StatusFound)
}

func oauthStateCookie(provider string) string {
	return "oauth_state_" + provider
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func exchangeCode(p oauthProvider, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", p.clientID)
	form.Set("client_secret", p.clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	req, _ := http.NewRequest(http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("no access token in response")
	}
	return tok.AccessToken, nil
}

func fetchEmail(provider string, p oauthProvider, accessToken string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	get := func(u string) ([]byte, error) {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "agentmesh") // GitHub rejects requests without UA
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		return io.ReadAll(res.Body)
	}

	if provider == "google" {
		body, err := get(p.userInfoURL)
		if err != nil {
			return "", err
		}
		var info struct {
			Email         string `json:"email"`
			VerifiedEmail bool   `json:"verified_email"`
		}
		json.Unmarshal(body, &info)
		// Only trust an email Google has confirmed the user owns.
		if info.Email == "" || !info.VerifiedEmail {
			return "", fmt.Errorf("email not verified")
		}
		return info.Email, nil
	}

	// github: ignore the (possibly unverified) profile email and require a
	// primary, verified address from /user/emails.
	body, err := get("https://api.github.com/user/emails")
	if err != nil {
		return "", err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	json.Unmarshal(body, &emails)
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}
	return "", fmt.Errorf("no verified email")
}
