package nodes_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestSlackAction_PostsMessageText(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "s1", Type: models.NodeTypeAction, Template: "slack",
		Secrets: map[string]string{"slackWebhookURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", []byte(`"hello from agent"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "slack_sent" {
		t.Errorf("want 'slack_sent', got %v", result)
	}
	if received["text"] != "hello from agent" {
		t.Errorf("want text field with message, got %v", received)
	}
}

func TestSlackAction_SkipsWhenNoWebhookURL(t *testing.T) {
	node := models.WorkflowNode{ID: "s2", Type: models.NodeTypeAction, Template: "slack"}
	rc := engine.NewRunContext("r1", []byte(`"hi"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "slack_skipped_no_webhook_url" {
		t.Errorf("want skip sentinel, got %v", result)
	}
}

func TestDiscordAction_PostsMessageContent(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "d1", Type: models.NodeTypeAction, Template: "discord",
		Secrets: map[string]string{"discordWebhookURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", []byte(`"hello discord"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "discord_sent" {
		t.Errorf("want 'discord_sent', got %v", result)
	}
	if received["content"] != "hello discord" {
		t.Errorf("want content field with message, got %v", received)
	}
}

func TestTeamsAction_PostsMessageCard(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "t1", Type: models.NodeTypeAction, Template: "teams",
		Secrets: map[string]string{"teamsWebhookURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", []byte(`"hello teams"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "teams_sent" {
		t.Errorf("want 'teams_sent', got %v", result)
	}
	if received["text"] != "hello teams" || received["@type"] != "MessageCard" {
		t.Errorf("want MessageCard payload with text, got %v", received)
	}
}

func TestGoogleChatAction_PostsMessageText(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "g1", Type: models.NodeTypeAction, Template: "google_chat",
		Secrets: map[string]string{"googleChatWebhookURL": srv.URL},
	}
	rc := engine.NewRunContext("r1", []byte(`"hello chat"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "google_chat_sent" {
		t.Errorf("want 'google_chat_sent', got %v", result)
	}
	if received["text"] != "hello chat" {
		t.Errorf("want text field with message, got %v", received)
	}
}

func TestNtfyAction_PostsPlainTextToTopic(t *testing.T) {
	var gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	node := models.WorkflowNode{
		ID: "nt1", Type: models.NodeTypeAction, Template: "ntfy",
		Config:  map[string]string{"ntfyTopic": "agentmesh-alerts", "ntfyServerURL": srv.URL},
		Secrets: map[string]string{"ntfyAuthToken": "tk_123"},
	}
	rc := engine.NewRunContext("r1", []byte(`"disk full"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "ntfy_sent" {
		t.Errorf("want 'ntfy_sent', got %v", result)
	}
	if gotPath != "/agentmesh-alerts" {
		t.Errorf("want path /agentmesh-alerts, got %q", gotPath)
	}
	if gotBody != "disk full" {
		t.Errorf("want plain-text body, got %q", gotBody)
	}
	if gotAuth != "Bearer tk_123" {
		t.Errorf("want bearer auth header, got %q", gotAuth)
	}
}

func TestNtfyAction_SkipsWhenNoTopic(t *testing.T) {
	node := models.WorkflowNode{ID: "nt2", Type: models.NodeTypeAction, Template: "ntfy"}
	rc := engine.NewRunContext("r1", []byte(`"hi"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "ntfy_skipped_no_topic" {
		t.Errorf("want skip sentinel, got %v", result)
	}
}
