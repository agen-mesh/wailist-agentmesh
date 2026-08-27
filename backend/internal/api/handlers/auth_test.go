package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/models"
)

func TestSignUpReturnsBadRequestOnEmptyEmail(t *testing.T) {
	d := &handlers.Deps{JWTSecret: "testsecret"}
	body, _ := json.Marshal(map[string]string{"email": "", "password": "validpassword"})
	req := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.SignUp(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestSignUpReturnsBadRequestOnShortPassword(t *testing.T) {
	d := &handlers.Deps{JWTSecret: "testsecret"}
	body, _ := json.Marshal(map[string]string{"email": "a@b.com", "password": "short"})
	req := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.SignUp(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

// changePassword drives the handler as an authenticated userID.
func changePassword(d *handlers.Deps, userID, current, next string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"currentPassword": current, "newPassword": next})
	req := httptest.NewRequest(http.MethodPost, "/auth/password", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, userID))
	w := httptest.NewRecorder()
	d.ChangePassword(w, req)
	return w
}

// seedPasswordUser creates a throwaway account whose password is known, so the
// verify-the-current-password path can be exercised for real rather than mocked.
// An empty password seeds the OAuth-only shape: password_hash = "".
func seedPasswordUser(t *testing.T, d *handlers.Deps, password string) models.User {
	t.Helper()
	var hash string
	if password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		hash = string(h)
	}
	email := fmt.Sprintf("pwtest-%d@example.test", time.Now().UnixNano())
	user, err := d.Store.CreateUser(context.Background(), email, hash)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

// The length floor is checked before any database read, so this is the one
// branch that holds without a test database — and it must match SignUp's.
func TestChangePasswordRejectsShortNewPassword(t *testing.T) {
	d := &handlers.Deps{JWTSecret: "testsecret"}
	if code := changePassword(d, "user1", "whatever", "short").Code; code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", code)
	}
}

func TestChangePasswordRejectsWrongCurrentPassword(t *testing.T) {
	d := testDeps(t)
	user := seedPasswordUser(t, d, "correct-horse")

	if code := changePassword(d, user.ID, "wrong-horse", "a-brand-new-password").Code; code != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d", code)
	}

	// The stored hash must be untouched after a failed attempt.
	after, err := d.Store.GetUserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("correct-horse")) != nil {
		t.Fatal("a rejected change must leave the original password working")
	}
}

// An OAuth-only account has an empty password_hash. Letting it set a password
// would add an unverified way into an account, so this is a 400, not a silent
// success — and distinctly not the 401 a wrong password gets.
func TestChangePasswordRejectsOAuthOnlyAccount(t *testing.T) {
	d := testDeps(t)
	user := seedPasswordUser(t, d, "")

	if code := changePassword(d, user.ID, "", "a-brand-new-password").Code; code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", code)
	}
}

func TestChangePasswordUpdatesTheStoredHash(t *testing.T) {
	d := testDeps(t)
	user := seedPasswordUser(t, d, "correct-horse")

	if code := changePassword(d, user.ID, "correct-horse", "battery-staple-1").Code; code != http.StatusNoContent {
		t.Fatalf("want 204 got %d", code)
	}

	after, err := d.Store.GetUserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("battery-staple-1")) != nil {
		t.Fatal("the new password should verify against the stored hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("correct-horse")) == nil {
		t.Fatal("the old password must stop working")
	}
}
