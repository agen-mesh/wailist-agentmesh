package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// ConnectorOAuthConfig describes one connector's OAuth 2.0 app registration.
// ClientIDEnvVal/ClientSecretEnvVal hold the already-resolved env var values
// (read once in main.go into Deps), not the env var names themselves.
type ConnectorOAuthConfig struct {
	AuthURL            string
	TokenURL           string
	Scope              string
	UsesPKCE           bool
	ClientIDEnvVal     string
	ClientSecretEnvVal string

	// TokenAuthStyle selects how exchangeConnectorCode presents client
	// credentials to TokenURL. "" (default) puts client_id/client_secret in
	// the form body, per the generic OAuth 2.0 authorization_code grant.
	// "basic" sends them instead as an HTTP Basic Authorization header and
	// omits them from the form body, which Notion's token endpoint requires.
	TokenAuthStyle string

	// ExtraAuthParams are added verbatim to the /authorize redirect's query
	// string, for providers that need fixed parameters beyond the generic
	// set below (e.g. Notion's required owner=user).
	ExtraAuthParams map[string]string
}

// testProviderOverrides holds provider configs injected by tests via
// SetConnectorProviderForTest, entirely separate from real Deps fields.
// registerConnectorProviders (below) merges these in first, then Tasks 3-14
// each add their real connector's entry (backed by that connector's actual
// Deps client id/secret fields) into the map it builds and returns. It is
// called once per request from the two handlers below so it always reflects
// the current Deps.
var testProviderOverrides = map[string]ConnectorOAuthConfig{}

// SetConnectorProviderForTest and ClearConnectorProviderForTest let tests
// register a fake provider without touching real Deps fields. Test-only.
func SetConnectorProviderForTest(name string, cfg ConnectorOAuthConfig) {
	testProviderOverrides[name] = cfg
}

func ClearConnectorProviderForTest(name string) {
	delete(testProviderOverrides, name)
}

// connectorTokenURLOverridesForTest lets a test point one real, already-wired
// provider's TokenURL (e.g. "airtable") at a local fake HTTP server, without
// touching any other field of that entry — so a test can exercise the actual
// registry entry (Scope, UsesPKCE, TokenAuthStyle, and Deps-sourced client
// id/secret all included) through ConnectorOAuthStart/Callback instead of
// re-declaring a look-alike config via SetConnectorProviderForTest. Test-only.
var connectorTokenURLOverridesForTest = map[string]string{}

func SetConnectorTokenURLForTest(name, url string) {
	connectorTokenURLOverridesForTest[name] = url
}

func ClearConnectorTokenURLForTest(name string) {
	delete(connectorTokenURLOverridesForTest, name)
}

// registerConnectorProviders builds the provider registry from Deps' client
// id/secret fields. Real provider entries (slack, github, notion, ...) are
// added to this map's returned literal by Tasks 3–14 as each connector is
// wired up.
func (d *Deps) registerConnectorProviders() map[string]ConnectorOAuthConfig {
	out := map[string]ConnectorOAuthConfig{}
	for k, v := range testProviderOverrides {
		out[k] = v
	}
	// Tasks 5-14 each append one entry here, e.g.:
	// out["notion"] = ConnectorOAuthConfig{AuthURL: ..., ClientIDEnvVal: d.NotionOAuthClientID, ...}
	out["slack"] = ConnectorOAuthConfig{
		AuthURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access",
		Scope: "chat:write", ClientIDEnvVal: d.SlackOAuthClientID, ClientSecretEnvVal: d.SlackOAuthClientSecret,
	}
	// repo is required (not a finer-grained scope) because classic GitHub
	// OAuth Apps only support coarse-grained scopes, and issue creation needs
	// write access to the repo. This is a distinct provider key/app from the
	// pre-existing "github" login OAuth app used for sign-in.
	out["github"] = ConnectorOAuthConfig{
		AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token",
		Scope: "repo", ClientIDEnvVal: d.GitHubConnectorClientID, ClientSecretEnvVal: d.GitHubConnectorClientSecret,
	}
	// Notion has no OAuth scope string at all (consent is page-level, granted
	// by which pages the user picks to share), requires owner=user on the
	// authorize redirect, and its token endpoint wants client credentials as
	// HTTP Basic auth rather than form fields — see connectors_productivity.go's
	// sendNotion and TokenAuthStyle's doc comment above.
	out["notion"] = ConnectorOAuthConfig{
		AuthURL: "https://api.notion.com/v1/oauth/authorize", TokenURL: "https://api.notion.com/v1/oauth/token",
		TokenAuthStyle: "basic", ExtraAuthParams: map[string]string{"owner": "user"},
		ClientIDEnvVal: d.NotionClientID, ClientSecretEnvVal: d.NotionClientSecret,
	}
	// Airtable requires PKCE for every client (no confidential-client
	// exemption), hence UsesPKCE here — this is the first provider entry to
	// actually exercise Task 1's PKCE code path.
	//
	// KNOWN GAP: Airtable access tokens expire after ~60 minutes and the
	// token response includes a refresh_token (valid ~60 days). Re-exchanging
	// that refresh token and writing the refreshed access token back onto the
	// node via Store.UpdateWorkflow before each connector call is NOT
	// implemented here — deliberately out of scope for this task. Without it,
	// this connector will silently stop working about an hour after linking.
	// Follow-up task must add refresh support.
	// Airtable is a confidential client (it has a client_secret at all), so
	// like Notion above its token endpoint requires Basic auth and rejects
	// client_id/client_secret in the form body — don't copy this style onto a
	// future public/no-secret connector that doesn't need it.
	out["airtable"] = ConnectorOAuthConfig{
		AuthURL: "https://airtable.com/oauth2/v1/authorize", TokenURL: "https://airtable.com/oauth2/v1/token",
		Scope: "data.records:write", UsesPKCE: true, TokenAuthStyle: "basic",
		ClientIDEnvVal: d.AirtableClientID, ClientSecretEnvVal: d.AirtableClientSecret,
	}
	// HubSpot's CRM object model binds Notes to Contacts rather than giving
	// notes their own scope — developers.hubspot.com/docs/api/crm/notes lists
	// crm.objects.contacts.read/.write as the scope requirement for this
	// endpoint (including POST), not a crm.objects.notes.* scope, so don't
	// "fix" this back to the more obvious-looking guess. HubSpot's token
	// endpoint takes client_id/client_secret as regular form fields (no
	// TokenAuthStyle needed), unlike Notion/Airtable above.
	//
	// KNOWN GAP: HubSpot access tokens expire after ~30 minutes and the token
	// response includes a refresh_token. Re-exchanging that refresh token and
	// writing the refreshed access token back onto the node via
	// Store.UpdateWorkflow before each connector call is NOT implemented here
	// — deliberately out of scope for this task, same as Airtable's gap
	// above. Without it, this connector will silently stop working about half
	// an hour after linking. Follow-up task must add refresh support.
	out["hubspot"] = ConnectorOAuthConfig{
		AuthURL: "https://app.hubspot.com/oauth/authorize", TokenURL: "https://api.hubapi.com/oauth/v1/token",
		Scope: "crm.objects.contacts.write", ClientIDEnvVal: d.HubSpotClientID, ClientSecretEnvVal: d.HubSpotClientSecret,
	}
	// developers.asana.com/docs/oauth: "default" is Asana's own documented
	// special scope value (full permissions, for apps registered without
	// finer-grained scopes) — not a placeholder left over from scaffolding.
	// PKCE is optional there, and the token endpoint takes client_id/
	// client_secret as regular form fields (no TokenAuthStyle needed), same as
	// Slack/GitHub/HubSpot above, unlike Notion/Airtable's Basic auth.
	//
	// KNOWN GAP: Asana access tokens expire after ~60 minutes and the token
	// response includes a refresh_token. Re-exchanging that refresh token and
	// writing the refreshed access token back onto the node via
	// Store.UpdateWorkflow before each connector call is NOT implemented here
	// — deliberately out of scope for this task, same as Airtable/HubSpot's
	// gaps above. Without it, this connector will silently stop working about
	// an hour after linking. Follow-up task must add refresh support.
	out["asana"] = ConnectorOAuthConfig{
		AuthURL: "https://app.asana.com/-/oauth_authorize", TokenURL: "https://app.asana.com/-/oauth_token",
		Scope: "default", ClientIDEnvVal: d.AsanaClientID, ClientSecretEnvVal: d.AsanaClientSecret,
	}
	// developer.clickup.com/docs/authentication: ClickUp has no OAuth scope
	// concept at all (consent is per-Workspace, granted by which Workspace the
	// user picks on the authorize screen, not a scope string), and its
	// /oauth/token endpoint takes client_id/client_secret/code as regular
	// form-body fields (no TokenAuthStyle needed), same as Slack/GitHub/
	// HubSpot/Asana above. PKCE is not offered. The authorize URL really is
	// just https://app.clickup.com/api?client_id=...&redirect_uri=...  — not
	// a conventional /oauth/authorize-shaped path — confirmed against the docs
	// above, not a guess.
	//
	// No KNOWN GAP here: per that same page, "The access token currently does
	// not expire" and no refresh_token is issued, unlike Airtable/HubSpot/
	// Asana above, so there is no refresh gap to document for this connector.
	out["clickup"] = ConnectorOAuthConfig{
		AuthURL: "https://app.clickup.com/api", TokenURL: "https://api.clickup.com/api/v2/oauth/token",
		ClientIDEnvVal: d.ClickUpClientID, ClientSecretEnvVal: d.ClickUpClientSecret,
	}
	for name, url := range connectorTokenURLOverridesForTest {
		if cfg, ok := out[name]; ok {
			cfg.TokenURL = url
			out[name] = cfg
		}
	}
	return out
}

func (d *Deps) connectorProviderConfig(name string) (ConnectorOAuthConfig, bool) {
	cfg, ok := d.registerConnectorProviders()[name]
	if !ok || cfg.ClientIDEnvVal == "" || cfg.ClientSecretEnvVal == "" {
		return ConnectorOAuthConfig{}, false
	}
	return cfg, true
}

func connectorSecretKey(provider string) string {
	return provider + "OAuthAccessToken"
}

func connectorRefreshKey(provider string) string {
	return provider + "OAuthRefreshToken"
}

func connectorExpiresConfigKey(provider string) string {
	return provider + "OAuthExpiresAt"
}

// connectorLinkClaims is signed into the OAuth `state` param so the callback
// can recover which node this authorization is for without a server-side
// pending-link table. The matching HttpOnly cookie (connectorStateCookie)
// provides CSRF binding: state must equal the cookie's own value, so a
// forged start-URL clicked by a victim can't complete using an attacker's
// authorization against the victim's own node.
//
// The PKCE code_verifier deliberately does NOT live here. This state JWT is
// also sent as the `state` query parameter on the redirect to the
// third-party's /authorize endpoint — a front-channel value that can end up
// in the provider's server logs, browser history, or a leaked referrer.
// HS256 signing gives integrity, not confidentiality, so anything placed in
// these claims should be treated as readable by whoever observes that URL.
// The verifier instead travels in its own separate HttpOnly cookie (see
// connectorVerifierCookieName) that's never echoed back to the provider.
type connectorLinkClaims struct {
	UserID     string `json:"sub"`
	WorkflowID string `json:"wf"`
	NodeID     string `json:"node"`
	Provider   string `json:"provider"`
	jwt.RegisteredClaims
}

const connectorLinkTTL = 10 * time.Minute

func connectorStateCookieName(provider string) string {
	return "connector_oauth_state_" + provider
}

// connectorVerifierCookieName names the separate HttpOnly cookie that carries
// the raw PKCE code_verifier across the Start -> Callback round-trip. Kept
// out of the signed state JWT (see connectorLinkClaims) because that JWT
// also travels as the `state` query param on the front channel to the
// provider; this cookie never leaves the browser-to-our-server path.
func connectorVerifierCookieName(provider string) string {
	return "connector_oauth_verifier_" + provider
}

// ConnectorOAuthStart redirects the browser to the provider's consent screen
// for linking workflowId/nodeId's connector node to the caller's account on
// that provider. Requires JWT auth (mounted in the protected router group) —
// the caller must be signed in, and must own the workflow being linked.
func (d *Deps) ConnectorOAuthStart(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	cfg, ok := d.connectorProviderConfig(provider)
	if !ok {
		respond.Error(w, http.StatusNotFound, "unknown or unconfigured connector")
		return
	}

	userID, _ := r.Context().Value(CtxUserID).(string)
	workflowID := r.URL.Query().Get("workflowId")
	nodeID := r.URL.Query().Get("nodeId")
	if workflowID == "" || nodeID == "" {
		respond.Error(w, http.StatusBadRequest, "workflowId and nodeId are required")
		return
	}

	wf, err := d.Store.GetWorkflow(r.Context(), workflowID)
	if err != nil || wf.UserID != userID {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	found := false
	for _, n := range wf.Nodes {
		if n.ID == nodeID {
			found = true
		}
	}
	if !found {
		respond.Error(w, http.StatusNotFound, "node not found")
		return
	}

	var verifier, challenge string
	if cfg.UsesPKCE {
		verifier, err = randURLSafe(64)
		if err != nil {
			d.connectorRedirectFail(w, r, workflowID, "internal")
			return
		}
		sum := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	}

	claims := connectorLinkClaims{
		UserID: userID, WorkflowID: workflowID, NodeID: nodeID, Provider: provider,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(connectorLinkTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	state, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(d.JWTSecret))
	if err != nil {
		d.connectorRedirectFail(w, r, workflowID, "internal")
		return
	}

	secure := strings.HasPrefix(d.BaseURL, "https")
	http.SetCookie(w, &http.Cookie{
		Name: connectorStateCookieName(provider), Value: state, Path: "/",
		MaxAge: int(connectorLinkTTL.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	if cfg.UsesPKCE {
		http.SetCookie(w, &http.Cookie{
			Name: connectorVerifierCookieName(provider), Value: verifier, Path: "/",
			MaxAge: int(connectorLinkTTL.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		})
	}

	q := url.Values{}
	q.Set("client_id", cfg.ClientIDEnvVal)
	q.Set("redirect_uri", d.connectorRedirectURI(provider))
	if cfg.Scope != "" {
		q.Set("scope", cfg.Scope)
	}
	q.Set("state", state)
	q.Set("response_type", "code")
	if cfg.UsesPKCE {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	for k, v := range cfg.ExtraAuthParams {
		q.Set(k, v)
	}
	http.Redirect(w, r, cfg.AuthURL+"?"+q.Encode(), http.StatusFound)
}

// ConnectorOAuthCallback verifies state, exchanges the code, and writes the
// resulting token onto the target node's Secrets/Config maps.
func (d *Deps) ConnectorOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	cfg, ok := d.connectorProviderConfig(provider)
	if !ok {
		respond.Error(w, http.StatusNotFound, "unknown or unconfigured connector")
		return
	}

	cookieName := connectorStateCookieName(provider)
	cookie, err := r.Cookie(cookieName)
	stateParam := r.URL.Query().Get("state")
	if err != nil || cookie.Value == "" || cookie.Value != stateParam {
		d.connectorRedirectFail(w, r, "", "invalid_state")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: strings.HasPrefix(d.BaseURL, "https"), SameSite: http.SameSiteLaxMode,
	})

	claims := &connectorLinkClaims{}
	_, err = jwt.ParseWithClaims(stateParam, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(d.JWTSecret), nil
	})
	if err != nil || claims.Provider != provider {
		d.connectorRedirectFail(w, r, "", "invalid_state")
		return
	}

	userID, _ := r.Context().Value(CtxUserID).(string)
	if userID != claims.UserID {
		d.connectorRedirectFail(w, r, claims.WorkflowID, "session_mismatch")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		d.connectorRedirectFail(w, r, claims.WorkflowID, "no_code")
		return
	}

	var verifier string
	if cfg.UsesPKCE {
		verifierCookieName := connectorVerifierCookieName(provider)
		verifierCookie, err := r.Cookie(verifierCookieName)
		if err != nil || verifierCookie.Value == "" {
			d.connectorRedirectFail(w, r, claims.WorkflowID, "invalid_state")
			return
		}
		verifier = verifierCookie.Value
		http.SetCookie(w, &http.Cookie{
			Name: verifierCookieName, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: strings.HasPrefix(d.BaseURL, "https"), SameSite: http.SameSiteLaxMode,
		})
	}

	accessToken, refreshToken, expiresIn, err := exchangeConnectorCode(cfg, code, d.connectorRedirectURI(provider), verifier)
	if err != nil {
		d.connectorRedirectFail(w, r, claims.WorkflowID, "token_exchange")
		return
	}

	if err := d.linkConnectorToken(r.Context(), claims.WorkflowID, claims.NodeID, provider, accessToken, refreshToken, expiresIn); err != nil {
		d.connectorRedirectFail(w, r, claims.WorkflowID, "link_failed")
		return
	}

	http.Redirect(w, r, d.FrontendURL+"/workflows/"+claims.WorkflowID+"?connected="+provider, http.StatusFound)
}

func (d *Deps) connectorRedirectURI(provider string) string {
	return strings.TrimRight(d.BaseURL, "/") + "/connectors/oauth/" + provider + "/callback"
}

func (d *Deps) connectorRedirectFail(w http.ResponseWriter, r *http.Request, workflowID, reason string) {
	dest := d.FrontendURL + "/workflows"
	if workflowID != "" {
		dest = d.FrontendURL + "/workflows/" + workflowID
	}
	http.Redirect(w, r, dest+"?connectError="+url.QueryEscape(reason), http.StatusFound)
}

// linkConnectorToken loads the workflow, writes the encrypted token (and
// optional refresh token / expiry) onto the target node, and persists the
// whole graph back — the same read-mutate-write shape UpdateWorkflow's
// handler already uses, just triggered from the OAuth callback instead of a
// frontend save.
func (d *Deps) linkConnectorToken(ctx context.Context, workflowID, nodeID, provider, accessToken, refreshToken string, expiresIn int) error {
	wf, err := d.Store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return err
	}
	idx := -1
	for i, n := range wf.Nodes {
		if n.ID == nodeID {
			idx = i
		}
	}
	if idx == -1 {
		return fmt.Errorf("node %s not found in workflow %s", nodeID, workflowID)
	}
	if wf.Nodes[idx].Secrets == nil {
		wf.Nodes[idx].Secrets = map[string]string{}
	}
	if wf.Nodes[idx].Config == nil {
		wf.Nodes[idx].Config = map[string]string{}
	}
	wf.Nodes[idx].Secrets[connectorSecretKey(provider)] = encryptField(accessToken, "", d.EncryptionKey)
	if refreshToken != "" {
		wf.Nodes[idx].Secrets[connectorRefreshKey(provider)] = encryptField(refreshToken, "", d.EncryptionKey)
	}
	if expiresIn > 0 {
		wf.Nodes[idx].Config[connectorExpiresConfigKey(provider)] = time.Now().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
	}
	_, err = d.Store.UpdateWorkflow(ctx, workflowID, wf.Name, models.WorkflowGraph{Nodes: wf.Nodes, Edges: wf.Edges})
	return err
}

// exchangeConnectorCode POSTs the standard OAuth 2.0 authorization_code grant
// and parses the standard token response shape (access_token/refresh_token/
// expires_in) that every in-scope provider in this plan uses. verifier is
// sent as code_verifier only when non-empty (PKCE providers).
func exchangeConnectorCode(cfg ConnectorOAuthConfig, code, redirectURI, verifier string) (accessToken, refreshToken string, expiresIn int, err error) {
	form := url.Values{}
	if cfg.TokenAuthStyle != "basic" {
		form.Set("client_id", cfg.ClientIDEnvVal)
		form.Set("client_secret", cfg.ClientSecretEnvVal)
	}
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}

	req, _ := http.NewRequest(http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if cfg.TokenAuthStyle == "basic" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cfg.ClientIDEnvVal+":"+cfg.ClientSecretEnvVal)))
	}

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", "", 0, err
	}
	if tok.AccessToken == "" {
		return "", "", 0, fmt.Errorf("no access token in response")
	}
	return tok.AccessToken, tok.RefreshToken, tok.ExpiresIn, nil
}

func randURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
