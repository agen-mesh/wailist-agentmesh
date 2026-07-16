package nodes_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestElevenLabsAction_GeneratesAudio(t *testing.T) {
	var gotPath, gotAPIKey string
	fakeAudio := []byte{0x1, 0x2, 0x3}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("xi-api-key")
		w.WriteHeader(http.StatusOK)
		w.Write(fakeAudio)
	}))
	defer srv.Close()
	nodes.SetElevenLabsAPIBaseForTest(srv.URL)
	defer nodes.SetElevenLabsAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "el1", Type: models.NodeTypeAction, Template: "elevenlabs",
		Secrets: map[string]string{"elevenlabsAPIKey": "xi_xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"read this aloud"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("want map result, got %T: %v", result, result)
	}
	if resMap["status"] != "elevenlabs_audio_generated" {
		t.Errorf("want status sentinel, got %v", resMap["status"])
	}
	wantB64 := base64.StdEncoding.EncodeToString(fakeAudio)
	if resMap["audioBase64"] != wantB64 {
		t.Errorf("want base64-encoded audio, got %v", resMap["audioBase64"])
	}
	if gotPath != "/v1/text-to-speech/21m00Tcm4TlvDq8ikWAM" {
		t.Errorf("want default voice id in path, got %q", gotPath)
	}
	if gotAPIKey != "xi_xxx" {
		t.Errorf("want xi-api-key header, got %q", gotAPIKey)
	}
}

func TestElevenLabsAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{
		ID: "el2", Type: models.NodeTypeAction, Template: "elevenlabs",
	}
	rc := engine.NewRunContext("r1", []byte(`"read this aloud"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	resStr, ok := result.(string)
	if !ok {
		t.Fatalf("want string result, got %T: %v", result, result)
	}
	if resStr != "elevenlabs_skipped_no_api_key" {
		t.Errorf("want skip sentinel, got %v", resStr)
	}
}

func TestElevenLabsAction_RejectsBlockedURL(t *testing.T) {
	blockErr := errors.New("requests to private/internal addresses are not allowed")
	nodes.SetURLValidatorForTest(func(string) error { return blockErr })
	// Restore the package-wide permissive validator (set once in TestMain)
	// rather than passing nil, which would flip global state to the real
	// strict validator for every test that runs after this one in the binary.
	defer nodes.SetURLValidatorForTest(func(string) error { return nil })

	node := models.WorkflowNode{
		ID: "el3", Type: models.NodeTypeAction, Template: "elevenlabs",
		Secrets: map[string]string{"elevenlabsAPIKey": "xi_xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"read this aloud"`))
	_, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err == nil {
		t.Fatal("want error when urlValidator rejects the target, got nil")
	}
	if !strings.Contains(err.Error(), "private/internal addresses") {
		t.Errorf("want validator error to propagate, got %v", err)
	}
}
