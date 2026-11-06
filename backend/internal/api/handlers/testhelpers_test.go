package handlers_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
)

// randSuffix returns a timestamp suffix so test fixtures get unique emails.
func randSuffix(t *testing.T) string {
	t.Helper()
	return time.Now().Format("20060102150405.000000000")
}

// withUser attaches an authenticated user id to a request's context.
func withUser(r *http.Request, userID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), handlers.CtxUserID, userID))
}
