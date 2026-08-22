package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

const authCookieName = "agentmesh_token"

// uiCookieName mirrors the frontend's own agentmesh_ui cookie (see
// useAuth.ts's setUICookie / middleware.ts) — a non-HttpOnly signal cookie
// the Next.js middleware gates protected routes on, since it can't read the
// HttpOnly agentmesh_token cookie set on the backend's cross-site response.
// Password sign-in/signup set it client-side right after a successful call,
// but OAuth is a pure server-redirect chain with no client JS in the loop,
// so OAuthCallback has to set it here or the middleware bounces a freshly
// signed-in OAuth user straight back to /signin.
const uiCookieName = "agentmesh_ui"

func (d *Deps) setUICookie(w http.ResponseWriter) {
	secure := strings.HasPrefix(os.Getenv("BASE_URL"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     uiCookieName,
		Value:    "1",
		Path:     "/",
		MaxAge:   int(tokenTTL.Seconds()),
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (d *Deps) setAuthCookie(w http.ResponseWriter, token string) {
	secure := strings.HasPrefix(os.Getenv("BASE_URL"), "https")
	sameSite := http.SameSiteLaxMode
	if secure {
		// SameSite=None is required for cross-site cookies (different subdomain
		// frontend/backend); it must be paired with Secure.
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(tokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	})
}

func (d *Deps) clearAuthCookie(w http.ResponseWriter) {
	secure := strings.HasPrefix(os.Getenv("BASE_URL"), "https")
	sameSite := http.SameSiteLaxMode
	if secure {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	})
}

// dummyHash is used in SignIn to keep response time constant even when the email doesn't exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-agentmesh"), bcrypt.DefaultCost)

const tokenTTL = 7 * 24 * time.Hour

type authClaims struct {
	UserID string `json:"sub"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

func (d *Deps) SignUp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
		Org      string `json:"org"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	body.Name = strings.TrimSpace(body.Name)
	body.Org = strings.TrimSpace(body.Org)
	if body.Email == "" || !strings.Contains(body.Email, "@") {
		respond.Error(w, http.StatusBadRequest, "valid email required")
		return
	}
	if len(body.Password) < 8 {
		respond.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if body.Name == "" {
		respond.Error(w, http.StatusBadRequest, "name required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := d.Store.CreateUser(r.Context(), body.Email, string(hash))
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			respond.Error(w, http.StatusConflict, "email already registered")
			return
		}
		log.Printf("create user: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Password signup collects name/org up front, unlike OAuth (which only
	// gets a verified email from the provider) — persist them immediately
	// rather than sending this user through the OAuth onboarding prompt too.
	user, err = d.Store.UpdateProfile(r.Context(), user.ID, body.Name, body.Org)
	if err != nil {
		log.Printf("set profile on signup: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	token, err := d.issueToken(user)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	d.setAuthCookie(w, token)
	respond.JSON(w, http.StatusCreated, map[string]any{})
}

func (d *Deps) SignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	if body.Email == "" || body.Password == "" {
		respond.Error(w, http.StatusBadRequest, "email and password required")
		return
	}

	user, lookupErr := d.Store.GetUserByEmail(r.Context(), body.Email)
	hash := []byte(user.PasswordHash)
	if lookupErr != nil {
		hash = dummyHash
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(body.Password)) != nil || lookupErr != nil {
		respond.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := d.issueToken(user)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	d.setAuthCookie(w, token)
	respond.JSON(w, http.StatusOK, map[string]any{})
}

func (d *Deps) SignOut(w http.ResponseWriter, r *http.Request) {
	d.clearAuthCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) Me(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	user, err := d.Store.GetUserByID(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "not found")
		return
	}
	// Carried on /auth/me rather than fetched separately, because every page
	// already calls this endpoint once and none of them should gain a second
	// request for a preference that is USD for most accounts. Costs one indexed
	// primary-key lookup; a failure here must not sign the user out, so it
	// degrades to the default rather than propagating.
	displayCurrency := models.DefaultCurrency
	if settings, sErr := d.Store.GetUserSettings(r.Context(), userID); sErr == nil {
		displayCurrency = settings.DisplayCurrency
	} else {
		log.Printf("me: display currency: %v", sErr)
	}

	respond.JSON(w, http.StatusOK, map[string]any{
		"id":              user.ID,
		"email":           user.Email,
		"name":            user.Name,
		"orgName":         user.OrgName,
		"displayCurrency": displayCurrency,
		// Already selected by GetUserByID and previously dropped here. The
		// settings page shows it as "member since"; nothing else needs it.
		"createdAt": user.CreatedAt,
		// OAuth accounts are created with no name — the frontend prompts for
		// name+org once, right after the provider redirect lands them here.
		"needsOnboarding": user.Name == "",
	})
}

// UpdateProfile sets the signed-in user's display name and organization
// name. Used by the post-OAuth onboarding prompt (see Me's needsOnboarding),
// and safe to call again later as a general profile edit.
func (d *Deps) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)

	var body struct {
		Name    string `json:"name"`
		OrgName string `json:"orgName"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	body.Name = strings.TrimSpace(body.Name)
	body.OrgName = strings.TrimSpace(body.OrgName)
	if body.Name == "" {
		respond.Error(w, http.StatusBadRequest, "name required")
		return
	}

	user, err := d.Store.UpdateProfile(r.Context(), userID, body.Name, body.OrgName)
	if err != nil {
		log.Printf("update profile: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{
		"id":              user.ID,
		"email":           user.Email,
		"name":            user.Name,
		"orgName":         user.OrgName,
		"needsOnboarding": false,
	})
}

// ChangePassword sets a new password for the signed-in user after verifying
// the current one.
//
// Two cases are deliberately distinct. A wrong current password is 401 — the
// caller is authenticated but failed to prove they know the existing secret.
// An OAuth-only account is 400: it has no password to verify (password_hash is
// empty, so bcrypt would reject anything), and quietly letting it set one would
// grow a second, unverified way into an account whose email we never confirmed.
//
// The session cookie is left alone. With no session table and no token
// blacklist there is nothing to revoke, so pretending a password change signs
// out other devices would be a lie — see the settings plan's §2.4.
func (d *Deps) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)

	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if len(body.NewPassword) < 8 {
		respond.Error(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}

	user, err := d.Store.GetUserByID(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "not found")
		return
	}
	if user.PasswordHash == "" {
		respond.Error(w, http.StatusBadRequest, "this account signs in with Google or GitHub and has no password to change")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.CurrentPassword)) != nil {
		respond.Error(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	updated, err := d.Store.UpdatePassword(r.Context(), userID, string(hash))
	if err != nil {
		log.Printf("update password: %v", err)
		respond.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !updated {
		respond.Error(w, http.StatusUnauthorized, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) issueToken(user models.User) (string, error) {
	if len(d.JWTSecret) < 32 {
		return "", errors.New("jwt secret not configured")
	}
	claims := authClaims{
		UserID: user.ID,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(d.JWTSecret))
}
