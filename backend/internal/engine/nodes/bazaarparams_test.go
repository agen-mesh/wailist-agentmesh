package nodes_test

import (
	"encoding/json"
	"testing"

	"github.com/agentmesh/backend/internal/engine/nodes"
)

// realCanixChallenge is the verbatim Bazaar extension canix402-api.compx.io
// serves in its live Payment-Required header (captured 2026-08-02), trimmed
// to the fields the extraction reads. Kept as raw JSON rather than a
// hand-built map so this stays a test of what a real target actually sends.
const realCanixChallenge = `{
  "extensions": {
    "bazaar": {
      "info": {
        "input": {
          "method": "GET",
          "queryParams": {"address": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAY5HFKQ"},
          "type": "http"
        },
        "output": {"example": {"data": []}, "type": "json"}
      },
      "schema": {
        "properties": {
          "input": {
            "properties": {
              "method": {"enum": ["GET"], "type": "string"},
              "queryParams": {
                "properties": {
                  "address": {"type": "string"},
                  "limit": {"type": "string"}
                },
                "required": ["address"],
                "type": "object"
              },
              "type": {"const": "http", "type": "string"}
            },
            "required": ["type", "method"],
            "type": "object"
          }
        }
      }
    }
  },
  "x402Version": 2
}`

func challengeMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("bad test fixture: %v", err)
	}
	return m
}

func TestParamsFromChallengeReadsRealBazaarExtension(t *testing.T) {
	params, method, in := nodes.ParamsFromChallenge(challengeMap(t, realCanixChallenge))

	if method != "GET" {
		t.Errorf("method: want GET, got %q", method)
	}
	if in != "query" {
		t.Errorf("in: want query, got %q", in)
	}
	if len(params) != 2 {
		t.Fatalf("want 2 params (address, limit), got %d: %+v", len(params), params)
	}

	// Sorted by name, so address precedes limit deterministically.
	if params[0].Name != "address" || params[1].Name != "limit" {
		t.Fatalf("want [address limit] in order, got [%s %s]", params[0].Name, params[1].Name)
	}
	// required comes from the schema's required list, not from the example
	// object -- both params have an entry in schema.properties, only one is
	// required, and getting this backwards would make the canvas demand a
	// value the target treats as optional (or vice versa).
	if !params[0].Required {
		t.Error("address should be required (listed in schema required)")
	}
	if params[1].Required {
		t.Error("limit should be optional (absent from schema required)")
	}
	// The example is a placeholder (the Algorand zero address) and must never
	// become a usable default -- sending it would spend real money asking
	// about an account nobody owns.
	if params[0].Default != "" {
		t.Errorf("example value must not become a Default, got %q", params[0].Default)
	}
	if params[0].Description == "" {
		t.Error("want the example surfaced as a description hint")
	}
}

func TestParamsFromChallengeNoBazaarExtension(t *testing.T) {
	// A perfectly valid v2 challenge that simply declares no inputs: this
	// must read as "no params", never as an error or a phantom field.
	params, _, in := nodes.ParamsFromChallenge(challengeMap(t, `{"accepts":[{"amount":"1"}],"x402Version":2}`))
	if len(params) != 0 {
		t.Errorf("want no params, got %+v", params)
	}
	if in != "" {
		t.Errorf("want empty location, got %q", in)
	}
}

func TestParamsFromChallengeBodyParams(t *testing.T) {
	// A POST-shaped target: params belong in the body, not the query string,
	// and the caller has to be told which so it builds the right request.
	raw := `{"extensions":{"bazaar":{
	  "info":{"input":{"method":"POST","body":{"prompt":"hello"},"type":"http"}},
	  "schema":{"properties":{"input":{"properties":{"body":{
	    "properties":{"prompt":{"type":"string"}},"required":["prompt"]}}}}}
	}}}`
	params, method, in := nodes.ParamsFromChallenge(challengeMap(t, raw))
	if method != "POST" {
		t.Errorf("method: want POST, got %q", method)
	}
	if in != "body" {
		t.Errorf("in: want body, got %q", in)
	}
	if len(params) != 1 || params[0].Name != "prompt" || !params[0].Required {
		t.Fatalf("want one required 'prompt' param, got %+v", params)
	}
}

func TestParamsFromChallengeIgnoresNonMapExtension(t *testing.T) {
	// Defensive: extensions/bazaar are attacker-influenced fields on an
	// arbitrary third-party endpoint. A non-object here must return empty,
	// not panic on a type assertion.
	for _, raw := range []string{
		`{"extensions":"nope"}`,
		`{"extensions":{"bazaar":42}}`,
		`{"extensions":{"bazaar":{"info":[1,2]}}}`,
		`{"extensions":{"bazaar":{"schema":{"properties":{"input":"x"}}}}}`,
	} {
		params, _, _ := nodes.ParamsFromChallenge(challengeMap(t, raw))
		if len(params) != 0 {
			t.Errorf("%s: want no params, got %+v", raw, params)
		}
	}
}
