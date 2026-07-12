package nodes_test

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestHubSpotAction_CreatesNote(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetHubSpotAPIBaseForTest(srv.URL)
	defer nodes.SetHubSpotAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "hs1", Type: models.NodeTypeAction, Template: "hubspot",
		Secrets: map[string]string{"hubspotAPIKey": "pat-na1-xxx"},
	}
	rc := engine.NewRunContext("r1", []byte(`"follow up with lead"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hubspot_note_created" {
		t.Errorf("want 'hubspot_note_created', got %v", result)
	}
	if gotAuth != "Bearer pat-na1-xxx" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	props, _ := gotBody["properties"].(map[string]any)
	if props["hs_note_body"] != "follow up with lead" {
		t.Errorf("want note body from message, got %v", gotBody)
	}
}

func TestHubSpotAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{
		ID: "hs2", Type: models.NodeTypeAction, Template: "hubspot",
	}
	rc := engine.NewRunContext("r1", []byte(`"follow up with lead"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hubspot_skipped_no_api_key" {
		t.Errorf("want 'hubspot_skipped_no_api_key', got %v", result)
	}
}

func TestMailchimpAction_AddsSubscriber(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	nodes.SetMailchimpAPIBaseForTest(srv.URL)
	defer nodes.SetMailchimpAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "mc1", Type: models.NodeTypeAction, Template: "mailchimp",
		Secrets: map[string]string{"mailchimpAPIKey": "abc123-us21"},
		Config:  map[string]string{"mailchimpListID": "list42", "mailchimpEmail": "New.User@Example.com"},
	}
	rc := engine.NewRunContext("r1", []byte(`"signup"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "mailchimp_subscriber_added" {
		t.Errorf("want 'mailchimp_subscriber_added', got %v", result)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("want PUT (upsert), got %s", gotMethod)
	}
	wantHash := md5.Sum([]byte("new.user@example.com"))
	wantPath := "/3.0/lists/list42/members/" + hex.EncodeToString(wantHash[:])
	if gotPath != wantPath {
		t.Errorf("want subscriber-hash path %q, got %q", wantPath, gotPath)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("anystring:abc123-us21"))
	if gotAuth != wantAuth {
		t.Errorf("want basic auth, got %q", gotAuth)
	}
	if gotBody["email_address"] != "New.User@Example.com" {
		t.Errorf("want original-case email in body, got %v", gotBody)
	}
}

func TestMailchimpDatacenter(t *testing.T) {
	dc, err := nodes.MailchimpDatacenterForTest("abc123-us21")
	if err != nil || dc != "us21" {
		t.Errorf("want 'us21', got %q err=%v", dc, err)
	}
	if _, err := nodes.MailchimpDatacenterForTest("nodashesheresuffix"); err == nil {
		t.Error("want error for malformed key")
	}
}

func TestMailchimpAction_SkipsWhenNoAPIKey(t *testing.T) {
	node := models.WorkflowNode{
		ID: "mc2", Type: models.NodeTypeAction, Template: "mailchimp",
		Config: map[string]string{"mailchimpListID": "list42", "mailchimpEmail": "user@example.com"},
	}
	rc := engine.NewRunContext("r1", []byte(`"test"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "mailchimp_skipped_no_api_key" {
		t.Errorf("want 'mailchimp_skipped_no_api_key', got %v", result)
	}
}

func TestMailchimpAction_SkipsWhenNoListID(t *testing.T) {
	node := models.WorkflowNode{
		ID: "mc3", Type: models.NodeTypeAction, Template: "mailchimp",
		Secrets: map[string]string{"mailchimpAPIKey": "abc123-us21"},
		Config:  map[string]string{"mailchimpEmail": "user@example.com"},
	}
	rc := engine.NewRunContext("r1", []byte(`"test"`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "mailchimp_skipped_no_list_id" {
		t.Errorf("want 'mailchimp_skipped_no_list_id', got %v", result)
	}
}

func TestMailchimpAction_SkipsWhenNoEmail(t *testing.T) {
	node := models.WorkflowNode{
		ID: "mc4", Type: models.NodeTypeAction, Template: "mailchimp",
		Secrets: map[string]string{"mailchimpAPIKey": "abc123-us21"},
		Config:  map[string]string{"mailchimpListID": "list42"},
	}
	rc := engine.NewRunContext("r1", []byte(`""`))
	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "mailchimp_skipped_no_email" {
		t.Errorf("want 'mailchimp_skipped_no_email', got %v", result)
	}
}
