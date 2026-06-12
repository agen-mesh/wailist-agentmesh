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
		Org      string `json:"org"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	if body.Email == "" || !strings.Contains(body.Email, "@") {
		respond.Error(w, http.StatusBadRequest, "valid email required")
		return
	}
	if len(body.Password) < 8 {
		respond.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
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
	u, err := d.Store.GetUserByID(r.Context(), userID)
	if err != nil {
		respond.JSON(w, http.StatusOK, map[string]any{"id": userID, "credits": 0.0})
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"id": u.ID, "email": u.Email, "credits": u.Credits})
}

func (d *Deps) TopupCredits(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	newBalance, err := d.Store.TopupCredits(r.Context(), userID, 10.0)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"credits": newBalance})
}

func (d *Deps) GetSpend(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	total, last24h, err := d.Store.GetUserSpend(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"total": total, "last24h": last24h})
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
