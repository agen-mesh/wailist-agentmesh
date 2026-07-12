package nodes_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
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
