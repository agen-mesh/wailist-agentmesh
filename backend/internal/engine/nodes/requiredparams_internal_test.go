package nodes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func searchNode(endpoint string, defaults map[string]string) models.WorkflowNode {
	return models.WorkflowNode{
		ID: "s1", Type: models.NodeTypeTool402, Endpoint: endpoint, Method: "GET",
		DiscoveredParams: []models.ParamDef{
			{Name: "query", Type: "string", Required: true},
			{Name: "domain_type", Type: "string"},
		},
		ParamDefaults: defaults,
	}
}

// A standalone paid step with its required query left blank was paid for and
// then rejected by the endpoint (400 "query: expected string, received
// undefined"). It must now fail before the endpoint is even contacted.
func TestMissingRequiredParamFailsBeforeAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	SetURLValidatorForTest(func(string) error { return nil })

	_, err := ExecuteTool402V2(context.Background(), searchNode(srv.URL, map[string]string{"query": "", "domain_type": "news"}), emptyRunContext{}, models.AgentWallet{}, nil, X402RelayConfig{})
	if err == nil || !strings.Contains(err.Error(), "query is required") || !strings.Contains(err.Error(), "nothing was paid") {
		t.Fatalf("want a required-param error, got %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("endpoint was contacted %d times; it must not be probed or paid", hits.Load())
	}
}

func TestMissingRequiredParamsSeesEveryValueSource(t *testing.T) {
	if m := missingRequiredParams(searchNode("https://x.test/s", map[string]string{"query": "whitechain"})); len(m) != 0 {
		t.Errorf("param field value not counted: %v", m)
	}
	// The agent path appends its arguments to the endpoint URL.
	if m := missingRequiredParams(searchNode("https://x.test/s?query=whitechain", nil)); len(m) != 0 {
		t.Errorf("URL query value not counted: %v", m)
	}
	n := searchNode("https://x.test/s", nil)
	n.CustomParams = []models.CustomParam{{Name: "query", Kind: "text", Value: "x"}}
	if m := missingRequiredParams(n); len(m) != 0 {
		t.Errorf("custom param value not counted: %v", m)
	}
	if m := missingRequiredParams(searchNode("https://x.test/s", nil)); len(m) != 1 || m[0] != "query" {
		t.Errorf("want [query] missing, got %v", m)
	}
}

type msgRC struct{ emptyRunContext }

func (msgRC) Message() string { return "whitechain news" }

func TestParamTemplatesResolveFromThePreviousStep(t *testing.T) {
	n := resolveParamTemplates(searchNode("https://x.test/s", map[string]string{"query": "{{ result }}"}), msgRC{})
	if n.ParamDefaults["query"] != "whitechain news" {
		t.Fatalf("query = %q, want the previous step's output", n.ParamDefaults["query"])
	}
}
