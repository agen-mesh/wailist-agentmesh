package nodes_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestCalculator(t *testing.T) {
	node := models.WorkflowNode{ID: "t1", Type: models.NodeTypeTool, Template: "calc", URL: "2 + 2 * 3"}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	if result != "8" {
		t.Fatalf("want 8 got %v", result)
	}
}

func TestDatetime(t *testing.T) {
	node := models.WorkflowNode{ID: "t2", Type: models.NodeTypeTool, Template: "datetime"}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := result.(string)
	if !ok || !strings.Contains(s, "T") {
		t.Fatalf("want RFC3339 got %v", result)
	}
}

func TestHTTPTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	node := models.WorkflowNode{ID: "t3", Type: models.NodeTypeTool, Template: "http", URL: srv.URL, Method: "GET"}
	rc := engine.NewRunContext("r1", nil)
	result, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]any)
	if !ok || m["status"] != "ok" {
		t.Fatalf("want {status:ok} got %v", result)
	}
}
