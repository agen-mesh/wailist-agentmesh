package nodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

// stubResend creates an httptest server that acts like Resend and captures the
// Authorization header used in the call. Returns the server and a pointer to the
// captured header value.
func stubResend(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	captured := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = r.Header.Get("Authorization")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "mock-email-id"})
	}))
	// Patch the Resend URL that sendViaResend calls.
	nodes.SetResendBaseURLForTest(srv.URL)
	t.Cleanup(func() { nodes.SetResendBaseURLForTest("") })
	return srv, captured
}

// TestEmailUsesNodeKey verifies that the node's own email API key is used when
// UseOurEmail=false.
func TestEmailUsesNodeKey(t *testing.T) {
	srv, capturedAuth := stubResend(t)
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "e1", Type: models.NodeTypeAction, Template: "email",
		EmailTo:      "test@example.com",
		EmailFrom:    "bot@example.com",
		EmailAPIKey:  "node-resend-key",
		UseOurEmail:  false,
	}
	rc := engine.NewRunContext("r1", []byte(`"agent output"`))
	_, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if *capturedAuth != "Bearer node-resend-key" {
		t.Fatalf("expected node key, got %q", *capturedAuth)
	}
}

// TestEmailUsesPlatformKey verifies that PLATFORM_RESEND_KEY is used when
// UseOurEmail=true, overriding the node's own key.
func TestEmailUsesPlatformKey(t *testing.T) {
	os.Setenv("PLATFORM_RESEND_KEY", "platform-resend-key")
	defer os.Unsetenv("PLATFORM_RESEND_KEY")

	srv, capturedAuth := stubResend(t)
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "e2", Type: models.NodeTypeAction, Template: "email",
		EmailTo:     "test@example.com",
		EmailFrom:   "bot@example.com",
		EmailAPIKey: "node-resend-key",
		UseOurEmail: true, // platform key should win
	}
	rc := engine.NewRunContext("r1", []byte(`"agent output"`))
	_, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if *capturedAuth != "Bearer platform-resend-key" {
		t.Fatalf("expected platform key, got %q", *capturedAuth)
	}
}

// TestEmailEmptyKeyFallsToPlatform verifies that an empty node key falls back to
// PLATFORM_RESEND_KEY rather than skipping the send.
func TestEmailEmptyKeyFallsToPlatform(t *testing.T) {
	os.Setenv("PLATFORM_RESEND_KEY", "platform-fallback-key")
	defer os.Unsetenv("PLATFORM_RESEND_KEY")

	srv, capturedAuth := stubResend(t)
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "e3", Type: models.NodeTypeAction, Template: "email",
		EmailTo:    "test@example.com",
		EmailFrom:  "bot@example.com",
		EmailAPIKey: "", // empty — falls back to platform
	}
	rc := engine.NewRunContext("r1", []byte(`"agent output"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result == "email_skipped_no_api_key" {
		t.Fatal("should not skip when platform key is set")
	}
	if *capturedAuth != "Bearer platform-fallback-key" {
		t.Fatalf("expected platform fallback key, got %q", *capturedAuth)
	}
}

// TestEmailSkipsWhenNoPlatformKey verifies that if neither the node key nor
// PLATFORM_RESEND_KEY is set, the action skips gracefully.
func TestEmailSkipsWhenNoPlatformKey(t *testing.T) {
	os.Unsetenv("PLATFORM_RESEND_KEY")

	node := models.WorkflowNode{
		ID: "e4", Type: models.NodeTypeAction, Template: "email",
		EmailTo:    "test@example.com",
		EmailAPIKey: "", // empty, no platform fallback
	}
	rc := engine.NewRunContext("r1", []byte(`"output"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "email_skipped_no_api_key" {
		t.Fatalf("expected skip, got %v", result)
	}
}
