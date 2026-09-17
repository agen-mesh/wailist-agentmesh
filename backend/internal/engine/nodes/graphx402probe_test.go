package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/bazaar"
	"github.com/agentmesh/backend/internal/models"
)

// x402Stub serves one canned response at any path and returns a catalog
// entry pointing at it, so a probe test exercises the real prober.
func x402Stub(t *testing.T, method string, handler http.HandlerFunc) bazaar.Resource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	SetURLValidatorForTest(func(string) error { return nil })
	// Restored to permissive, not to nil: every other test in this package
	// does the same, and they depend on it staying that way.
	t.Cleanup(func() { SetURLValidatorForTest(func(string) error { return nil }) })
	return bazaar.Resource{
		ID: "res-1", URL: srv.URL + "/v1/thing", Method: method,
		Description: "Live stock market index prices", Host: "stocks.example.com",
		AmountMicros: 5000, Asset: "31566704", SettleCount: 42,
	}
}

// challenge402 writes a real x402 v2 challenge demanding amount base units.
func challenge402(amount string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"x402Version":2,"accepts":[{"scheme":"exact","network":"algorand:x","amount":"` +
			amount + `","asset":"31566704","payTo":"ABC","maxTimeoutSeconds":60}]}`))
	}
}

func addOne(t *testing.T, r bazaar.Resource) (*models.WorkflowGraph, string, error) {
	t.Helper()
	x := newX402Session(func(context.Context) ([]bazaar.Resource, error) {
		return []bazaar.Resource{r}, nil
	})
	graph := &models.WorkflowGraph{}
	msg, err := x.add(context.Background(), graph, map[string]any{"id": r.ID})
	return graph, msg, err
}

// The catalog is a mirror, not a health check: an entry stays in it after
// the service behind it is gone. Adding one to the canvas without checking
// puts a dead paid endpoint in front of the user.
func TestAddX402NodeRefusesADeadEndpoint(t *testing.T) {
	r := x402Stub(t, "GET", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, r2(w))
	})
	graph, _, err := addOne(t, r)
	if err == nil {
		t.Fatal("a 404 endpoint must be refused")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("the refusal must say what happened, got %q", err)
	}
	if len(graph.Nodes) != 0 {
		t.Fatal("nothing may be added for a refused endpoint")
	}
}

func TestAddX402NodeRefusesAnUnreachableEndpoint(t *testing.T) {
	SetURLValidatorForTest(func(string) error { return nil })
	// Restored to permissive, not to nil: every other test in this package
	// does the same, and they depend on it staying that way.
	t.Cleanup(func() { SetURLValidatorForTest(func(string) error { return nil }) })
	r := bazaar.Resource{ID: "res-1", Method: "GET", AmountMicros: 5000, Asset: "31566704",
		// Reserved by RFC 6761 to never resolve.
		URL: "https://nothing.invalid/v1/thing", Host: "nothing.invalid"}
	graph, _, err := addOne(t, r)
	if err == nil {
		t.Fatal("an unreachable endpoint must be refused")
	}
	if len(graph.Nodes) != 0 {
		t.Fatal("nothing may be added for a refused endpoint")
	}
}

// The happy path: still gated, still the price the catalog advertises.
func TestAddX402NodeAddsAVerifiedEndpoint(t *testing.T) {
	r := x402Stub(t, "GET", challenge402("5000"))
	graph, msg, err := addOne(t, r)
	if err != nil {
		t.Fatalf("a live gated endpoint must be added: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("want one node, got %d", len(graph.Nodes))
	}
	if !strings.Contains(msg, "verified") {
		t.Errorf("the builder should be told the endpoint was checked, got %q", msg)
	}
}

// A price the endpoint no longer charges is what the user is quoted and
// billed against. The live challenge is the truth; the catalog is a mirror
// that can be hours stale.
func TestAddX402NodeTakesThePriceFromTheLiveChallenge(t *testing.T) {
	r := x402Stub(t, "GET", challenge402("250000"))
	graph, msg, err := addOne(t, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].Price != "0.25" {
		t.Fatalf("node price = %q, want the live 0.25", graph.Nodes[0].Price)
	}
	if !strings.Contains(msg, "0.25") {
		t.Errorf("the builder must be quoted the live price, got %q", msg)
	}
}

// An endpoint that answers 200 to an unpaid request is not gated. It may be
// genuinely free, or it may be an error page: either way the user should
// hear about it rather than be billed for a paid node that is not one.
func TestAddX402NodeWarnsWhenAnEndpointNoLongerAsksForPayment(t *testing.T) {
	r := x402Stub(t, "GET", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	graph, msg, err := addOne(t, r)
	if err != nil {
		t.Fatalf("an ungated endpoint is a warning, not a refusal: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("want one node, got %d", len(graph.Nodes))
	}
	if !strings.Contains(msg, "without asking for payment") {
		t.Errorf("the builder must be warned, got %q", msg)
	}
}

// A POST is probed with no body, so anything but a 402 or a hard 404 is
// inconclusive: the endpoint may simply be rejecting the empty request.
func TestAddX402NodeDoesNotRefuseAPostOnABadRequest(t *testing.T) {
	r := x402Stub(t, "POST", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing payload", http.StatusBadRequest)
	})
	graph, msg, err := addOne(t, r)
	if err != nil {
		t.Fatalf("an unprobeable POST must not be refused: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("want one node, got %d", len(graph.Nodes))
	}
	if !strings.Contains(msg, "could not be checked") {
		t.Errorf("the builder must be told the check was inconclusive, got %q", msg)
	}
}

// r2 keeps http.NotFound's signature happy without a real request.
func r2(http.ResponseWriter) *http.Request {
	req, _ := http.NewRequest("GET", "http://x/", nil)
	return req
}

// The model does the picking, so the evidence the ranking uses has to reach
// it, not just reorder the list behind its back.
func TestSearchX402ReportsTrustAlongsideEachResult(t *testing.T) {
	stale := bazaar.Resource{
		ID: "stale", Host: "old.example.com", URL: "https://old.example.com/v1",
		Description: "Algorand transaction history for an address", SettleCount: 900,
		FirstSeen: time.Now().AddDate(0, 0, -300).Format(time.RFC3339),
		LastSeen:  time.Now().AddDate(0, 0, -112).Format(time.RFC3339),
	}
	x := newX402Session(func(context.Context) ([]bazaar.Resource, error) {
		return []bazaar.Resource{stale}, nil
	})
	out, err := x.search(context.Background(), map[string]any{"query": "algorand transaction history"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var got struct {
		Results []struct {
			Trust           string `json:"trust"`
			LastPaidDaysAgo int    `json:"lastPaidDaysAgo"`
		} `json:"results"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Results) != 1 {
		t.Fatalf("want one result, got %d", len(got.Results))
	}
	if got.Results[0].Trust != "stale" {
		t.Errorf("trust = %q, want stale", got.Results[0].Trust)
	}
	if got.Results[0].LastPaidDaysAgo != 112 {
		t.Errorf("lastPaidDaysAgo = %d, want 112", got.Results[0].LastPaidDaysAgo)
	}
	if !strings.Contains(got.Note, "stale") {
		t.Errorf("the note must explain what the tiers mean, got %q", got.Note)
	}
}
