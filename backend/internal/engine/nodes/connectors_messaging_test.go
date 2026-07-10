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
