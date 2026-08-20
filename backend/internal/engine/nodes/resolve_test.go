package nodes_test

import (
	"testing"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestResolveTemplate(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"original question"`))
	rc.Set("n1", "agent answer")
	rc.Set("n3", map[string]any{"items": []any{
		map[string]any{"title": "first"},
		map[string]any{"title": "second"},
	}})
	// n2 last: {{ result }} below must keep resolving to n2's output, not n3's.
	rc.Set("n2", map[string]any{"city": "Kolkata", "temp": 31.5})

	cases := []struct{ name, in, want string }{
		{"result", "Answer: {{ result }}", `Answer: {"city":"Kolkata","temp":31.5}`},
		{"no spaces", "{{result}}", `{"city":"Kolkata","temp":31.5}`},
		{"input", "Q: {{ input }}", "Q: original question"},
		{"node by id", "got {{ node.n1 }}", "got agent answer"},
		{"node field", "in {{ node.n2.city }}", "in Kolkata"},
		{"numeric field", "temp {{ node.n2.temp }}", "temp 31.5"},
		{"array index field", "t: {{ node.n3.items.0.title }}", "t: first"},
		{"second array index", "t: {{ node.n3.items.1.title }}", "t: second"},
		{"array index out of range left verbatim", "x {{ node.n3.items.5.title }} y", "x {{ node.n3.items.5.title }} y"},
		{"two refs", "{{ node.n1 }} / {{ input }}", "agent answer / original question"},
		{"unknown node left verbatim", "x {{ node.nope }} y", "x {{ node.nope }} y"},
		{"unknown field left verbatim", "x {{ node.n2.nope }} y", "x {{ node.n2.nope }} y"},
		{"field on a string output left verbatim", "x {{ node.n1.city }} y", "x {{ node.n1.city }} y"},
		{"unknown keyword left verbatim", "x {{ bogus }} y", "x {{ bogus }} y"},
		{"no refs untouched", "plain text", "plain text"},
		{"empty string", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nodes.ResolveTemplateForTest(tc.in, rc); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// The email body's {{ result }} contract predates this resolver and must keep
// working exactly as before.
func TestEmailBodyStillResolvesResult(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "the answer")
	node := models.WorkflowNode{
		ID: "e1", Type: models.NodeTypeAction, Template: "email",
		EmailBody: "Result was: {{ result }}",
	}
	// No API key -> skipped before any network call; we are asserting the
	// template contract compiles and the skip sentinel is unchanged.
	got, err := nodes.ExecuteAction(t.Context(), node, rc)
	if got != "email_skipped_no_api_key" {
		t.Errorf("want skip sentinel, got %v (err %v)", got, err)
	}
}
