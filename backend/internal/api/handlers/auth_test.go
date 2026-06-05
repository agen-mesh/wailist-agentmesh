package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/api/handlers"
)

func TestSignIn(t *testing.T) {
	d := &handlers.Deps{}
	body, _ := json.Marshal(map[string]string{"email": "test@test.com", "password": "pass"})
	req := httptest.NewRequest(http.MethodPost, "/auth/signin", bytes.NewReader(body))
	w := httptest.NewRecorder()
	d.SignIn(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] == "" {
		t.Fatal("no token in response")
	}
}
