package nodes_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestTwilioAction_SendsSMS(t *testing.T) {
	var gotUser, gotPass, gotPath string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"sid":"SM123"}`))
	}))
	defer srv.Close()
	nodes.SetTwilioAPIBaseForTest(srv.URL)
	defer nodes.SetTwilioAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "tw1", Type: models.NodeTypeAction, Template: "twilio",
		Secrets: map[string]string{"twilioAuthToken": "authtok"},
		Config: map[string]string{
			"twilioAccountSID": "AC123",
			"twilioFrom":       "+15550001111",
			"twilioTo":         "+15550002222",
		},
	}
	rc := engine.NewRunContext("r1", []byte(`"deploy finished"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "twilio_sms_sent" {
		t.Errorf("want 'twilio_sms_sent', got %v", result)
	}
	if gotUser != "AC123" || gotPass != "authtok" {
		t.Errorf("want basic auth SID/token, got %q/%q", gotUser, gotPass)
	}
	if !strings.HasSuffix(gotPath, "/Accounts/AC123/Messages.json") {
		t.Errorf("want the account-scoped Messages path, got %q", gotPath)
	}
	if gotForm.Get("To") != "+15550002222" || gotForm.Get("From") != "+15550001111" {
		t.Errorf("To/From: got %v", gotForm)
	}
	if gotForm.Get("Body") != "deploy finished" {
		t.Errorf("Body should be the run output, got %q", gotForm.Get("Body"))
	}
}

func TestTwilioAction_SkipsWhenUnconfigured(t *testing.T) {
	cases := []struct {
		name string
		node models.WorkflowNode
		want string
	}{
		{"no token", models.WorkflowNode{
			Template: "twilio",
			Config:   map[string]string{"twilioAccountSID": "AC1", "twilioFrom": "+1", "twilioTo": "+2"},
		}, "twilio_skipped_no_auth_token"},
		{"no sid", models.WorkflowNode{
			Template: "twilio",
			Secrets:  map[string]string{"twilioAuthToken": "t"},
			Config:   map[string]string{"twilioFrom": "+1", "twilioTo": "+2"},
		}, "twilio_skipped_no_account_sid"},
		{"no recipient", models.WorkflowNode{
			Template: "twilio",
			Secrets:  map[string]string{"twilioAuthToken": "t"},
			Config:   map[string]string{"twilioAccountSID": "AC1", "twilioFrom": "+1"},
		}, "twilio_skipped_no_recipient"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.node.Type = models.NodeTypeAction
			rc := engine.NewRunContext("r1", nil)
			got, _ := nodes.ExecuteAction(context.Background(), tc.node, rc)
			if got != tc.want {
				t.Errorf("want %q, got %v", tc.want, got)
			}
		})
	}
}
