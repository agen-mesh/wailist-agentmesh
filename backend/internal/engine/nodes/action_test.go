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

func TestWebhookAction(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	node := models.WorkflowNode{ID: "a1", Type: models.NodeTypeAction, Template: "webhook", URL: srv.URL}
	rc := engine.NewRunContext("r1", []byte(`{"message":"test payload"}`))
	_, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if received == nil {
		t.Fatal("webhook not called")
	}
}

func TestLogAction(t *testing.T) {
	node := models.WorkflowNode{ID: "a2", Type: models.NodeTypeAction, Template: "log"}
	rc := engine.NewRunContext("r1", []byte(`"hello"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "logged" {
		t.Fatalf("want 'logged' got %v", result)
	}
}

func TestEmailAction_SendGridProvider(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	nodes.SetSendGridAPIBaseForTest(srv.URL)
	defer nodes.SetSendGridAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "e1", Type: models.NodeTypeAction, Template: "email",
		EmailProvider: "sendgrid", EmailAPIKey: "SG.xxx",
		EmailFrom: "AgentMesh <you@yourdomain.com>", EmailTo: "user@example.com", EmailSubject: "Result",
	}
	rc := engine.NewRunContext("r1", []byte(`"done"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "email_sent" {
		t.Errorf("want 'email_sent', got %v", result)
	}
	if gotAuth != "Bearer SG.xxx" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	from, _ := gotBody["from"].(map[string]any)
	if from["email"] != "you@yourdomain.com" || from["name"] != "AgentMesh" {
		t.Errorf("want parsed from name/email, got %v", from)
	}
}

func TestParseEmailAddress(t *testing.T) {
	name, email := nodes.ParseEmailAddressForTest("AgentMesh <you@yourdomain.com>")
	if name != "AgentMesh" || email != "you@yourdomain.com" {
		t.Errorf("want name=AgentMesh email=you@yourdomain.com, got name=%q email=%q", name, email)
	}
	name2, email2 := nodes.ParseEmailAddressForTest("plain@yourdomain.com")
	if name2 != "" || email2 != "plain@yourdomain.com" {
		t.Errorf("want name='' email=plain@yourdomain.com, got name=%q email=%q", name2, email2)
	}
}
