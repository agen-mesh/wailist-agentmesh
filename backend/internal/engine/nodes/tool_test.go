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

// Every tool template offered in the palette must reach a real
// implementation. ExecuteTool's default case silently echoes the upstream
// output (`return rc.Message(), nil`) with no error — a template that falls
// through to it renders as a successful node that did nothing, the same bug
// class TestEveryNewActionTemplateIsDispatched guards for actions. Each
// template is run unconfigured against a distinctive marker set as the
// upstream output; a template wired correctly either errors on missing
// config or transforms the marker into something else, but never returns it
// unchanged.
func TestEveryNewToolTemplateIsDispatched(t *testing.T) {
	const marker = "AGENTMESH_DISPATCH_MARKER_9f3c"
	templates := []string{
		"set", "json_extract", "crypto", "datetime", "xml",
		"template", "html_extract", "markdown", "quickchart",
	}
	for _, tpl := range templates {
		t.Run(tpl, func(t *testing.T) {
			rc := engine.NewRunContext("r1", nil)
			rc.Set("n0", marker)
			node := models.WorkflowNode{ID: "n1", Type: models.NodeTypeTool, Template: tpl}
			got, _ := nodes.ExecuteTool(context.Background(), node, rc)
			if got == marker {
				t.Errorf("template %q fell through to ExecuteTool's default branch — "+
					"it silently echoes the upstream output instead of doing anything", tpl)
			}
		})
	}
}
