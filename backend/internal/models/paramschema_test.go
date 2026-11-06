package models

import (
	"reflect"
	"strings"
	"testing"
)

// Shape copied from tinyx402's live /api/v1/search Bazaar schema
// (2026-11-07). The agent once called it with query omitted and a made-up
// domain_type because the importer kept only "example: ..." and marked every
// param optional.
func TestParamsFromSchemaKeepsRequiredEnumAndFormat(t *testing.T) {
	spec := map[string]any{
		"type":     "object",
		"required": []any{"query"},
		"properties": map[string]any{
			"query":       map[string]any{"type": "string", "minLength": 1.0, "maxLength": 2000.0, "description": "Search query"},
			"domain_type": map[string]any{"type": "string", "enum": []any{"web", "news", "research_paper"}},
			"after_date":  map[string]any{"type": "string", "pattern": `^\d{4}-\d{2}-\d{2}$`},
			"page":        map[string]any{"type": "integer", "minimum": 0.0, "maximum": 10.0},
		},
	}
	got := ParamsFromSchema(spec, map[string]any{"query": "x402", "domain_type": "web"})
	by := map[string]ParamDef{}
	for _, p := range got {
		by[p.Name] = p
	}
	if !by["query"].Required || by["domain_type"].Required {
		t.Fatalf("required not carried from the schema: %+v", got)
	}
	if !strings.Contains(by["query"].Description, "Search query") || !strings.Contains(by["query"].Description, "example: x402") {
		t.Errorf("query description = %q", by["query"].Description)
	}
	if !reflect.DeepEqual(by["domain_type"].Enum, []string{"web", "news", "research_paper"}) {
		t.Errorf("domain_type enum = %v", by["domain_type"].Enum)
	}
	if !strings.Contains(by["after_date"].Description, "YYYY-MM-DD") {
		t.Errorf("after_date description = %q", by["after_date"].Description)
	}
	if by["page"].Type != "integer" || !strings.Contains(by["page"].Description, "0 to 10") {
		t.Errorf("page = %+v", by["page"])
	}
	if got[0].Name != "after_date" {
		t.Errorf("params must be sorted by name, got first %q", got[0].Name)
	}
}
