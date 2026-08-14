package nodes_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestStripeAction_CreatesCustomer(t *testing.T) {
	var gotAuth, gotCT, gotPath string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"cus_123"}`))
	}))
	defer srv.Close()
	nodes.SetStripeAPIBaseForTest(srv.URL)
	defer nodes.SetStripeAPIBaseForTest("")

	node := models.WorkflowNode{
		ID: "st1", Type: models.NodeTypeAction, Template: "stripe",
		Secrets: map[string]string{"stripeAPIKey": "sk_test_xxx"},
		Config:  map[string]string{"stripeEmail": "buyer@example.com"},
	}
	rc := engine.NewRunContext("r1", []byte(`"new signup from the workflow"`))

	result, err := nodes.ExecuteAction(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "stripe_customer_created" {
		t.Errorf("want 'stripe_customer_created', got %v", result)
	}
	if gotAuth != "Bearer sk_test_xxx" {
		t.Errorf("want bearer auth, got %q", gotAuth)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("Stripe requires form encoding, got %q", gotCT)
	}
	if gotPath != "/v1/customers" {
		t.Errorf("want /v1/customers, got %q", gotPath)
	}
	if gotForm.Get("email") != "buyer@example.com" {
		t.Errorf("email: got %q", gotForm.Get("email"))
	}
	if gotForm.Get("description") != "new signup from the workflow" {
		t.Errorf("description should be the run output, got %q", gotForm.Get("description"))
	}
}

func TestStripeAction_SkipsWithoutAPIKey(t *testing.T) {
	node := models.WorkflowNode{ID: "st1", Type: models.NodeTypeAction, Template: "stripe"}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "stripe_skipped_no_api_key" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}

func TestStripeAction_SkipsWithoutEmail(t *testing.T) {
	node := models.WorkflowNode{
		ID: "st1", Type: models.NodeTypeAction, Template: "stripe",
		Secrets: map[string]string{"stripeAPIKey": "sk_test_xxx"},
	}
	rc := engine.NewRunContext("r1", nil)
	got, _ := nodes.ExecuteAction(context.Background(), node, rc)
	if got != "stripe_skipped_no_email" {
		t.Errorf("want skip sentinel, got %v", got)
	}
}
