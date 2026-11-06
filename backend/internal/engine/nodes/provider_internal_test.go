package nodes

import (
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// The LLM must be told which values an endpoint accepts, or it invents one
// (domain_type "latest") and the paid call is rejected.
func TestBuildFuncDeclsCarriesEnumAndRequired(t *testing.T) {
	decls := buildFuncDecls([]models.WorkflowNode{{
		ID: "t1", Name: "News Search", Type: models.NodeTypeTool402,
		DiscoveredParams: []models.ParamDef{
			{Name: "query", Type: "string", Required: true, Description: "Search query"},
			{Name: "domain_type", Type: "string", Enum: []string{"web", "news", "research_paper"}},
		},
	}})
	params := decls[0].Parameters
	if req, _ := params["required"].([]string); len(req) != 1 || req[0] != "query" {
		t.Fatalf("required = %v, want [query]", params["required"])
	}
	props := params["properties"].(map[string]any)
	enum, _ := props["domain_type"].(map[string]any)["enum"].([]string)
	if len(enum) != 3 || enum[1] != "news" {
		t.Fatalf("domain_type enum = %v", props["domain_type"])
	}
}
