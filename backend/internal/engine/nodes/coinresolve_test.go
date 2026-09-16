package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func TestResolveCoinReturnsRankedMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != "aerodrome" {
			t.Errorf("query = %q, want aerodrome", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"coins": []map[string]any{
			{"id": "aerodrome-finance", "symbol": "aero", "name": "Aerodrome Finance", "market_cap_rank": 102},
		}})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	got, err := resolveCoin(context.Background(), "aerodrome")
	if err != nil {
		t.Fatalf("resolveCoin: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches, want 1", len(got))
	}
	if got[0].ID != "aerodrome-finance" || got[0].Symbol != "AERO" || got[0].Rank != 102 {
		t.Errorf("match = %+v", got[0])
	}
}

// The case that started this: a token nobody lists. An empty result is an
// answer, not a failure -- the builder has to be able to tell the user.
func TestResolveCoinOnAnUnlistedTokenReturnsNoMatchesAndNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"coins": []any{}})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	got, err := resolveCoin(context.Background(), "myriad")
	if err != nil {
		t.Fatalf("an unlisted token must not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d matches for an unlisted token", len(got))
	}
}

func TestResolveCoinRequiresAQuery(t *testing.T) {
	if _, err := resolveCoin(context.Background(), "   "); err == nil {
		t.Fatal("an empty query was accepted")
	}
}

// CoinGecko lists every coin whose name contains the query; only the first
// few go back to the model.
func TestResolveCoinCapsTheMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coins := make([]map[string]any, 0, 20)
		for i := 0; i < 20; i++ {
			coins = append(coins, map[string]any{"id": "coin-" + string(rune('a'+i)), "symbol": "c", "name": "Coin"})
		}
		json.NewEncoder(w).Encode(map[string]any{"coins": coins})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	got, err := resolveCoin(context.Background(), "coin")
	if err != nil {
		t.Fatalf("resolveCoin: %v", err)
	}
	if len(got) != maxCoinMatches {
		t.Fatalf("got %d matches, want %d", len(got), maxCoinMatches)
	}
}

// resolve_coin through the build dispatcher: matches come back with their
// ids, and an empty result is reported as not_listed rather than an error.
func TestResolveCoinThroughTheBuildDispatcher(t *testing.T) {
	var coins []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"coins": coins})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	resolved := map[string]bool{}
	call := func(q string) string {
		res := dispatchBuildCall(context.Background(), nil, geminiFuncCall{
			name: "resolve_coin", args: map[string]any{"query": q},
		}, "k", nil, nil, resolved, &testTracker{})
		text, _ := res["result"].(string)
		return text
	}

	coins = []map[string]any{{"id": "bitcoin", "symbol": "btc", "name": "Bitcoin", "market_cap_rank": 1}}
	if got := call("bitcoin"); !containsAll(got, `"id":"bitcoin"`, "use the id field") {
		t.Errorf("match result = %q", got)
	}
	if !resolved["bitcoin"] {
		t.Error("a returned match was not recorded as resolved")
	}

	coins = []map[string]any{}
	got := call("myriad")
	if !containsAll(got, "not_listed:", "myriad") {
		t.Errorf("unlisted result = %q", got)
	}
	step := finishedStep(nil, "resolve_coin", map[string]any{"query": "myriad"}, map[string]any{"result": got})
	if step.Status != "error" {
		t.Errorf("an unlisted token shows as %q, want error", step.Status)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestAddNodeRefusesACoinGeckoIDThatWasNeverResolved(t *testing.T) {
	graph := &models.WorkflowGraph{}
	resolved := map[string]bool{}

	_, err := addGraphNodeResolved(graph, map[string]any{
		"type": "action", "template": "coingecko", "name": "Price",
		"config": map[string]any{"cgIDs": "myriad"},
	}, resolved)
	if err == nil {
		t.Fatal("a guessed coin id was accepted")
	}
	if !strings.Contains(err.Error(), "resolve_coin") {
		t.Errorf("the refusal does not name the tool to use, got: %v", err)
	}
	if !strings.Contains(err.Error(), "myriad") {
		t.Errorf("the refusal does not name the id it rejected, got: %v", err)
	}
	if len(graph.Nodes) != 0 {
		t.Error("the node was added anyway")
	}
}

func TestAddNodeAcceptsACoinGeckoIDThatWasResolved(t *testing.T) {
	graph := &models.WorkflowGraph{}
	resolved := map[string]bool{"aerodrome-finance": true}

	if _, err := addGraphNodeResolved(graph, map[string]any{
		"type": "action", "template": "coingecko", "name": "Price",
		"config": map[string]any{"cgIDs": "aerodrome-finance"},
	}, resolved); err != nil {
		t.Fatalf("a resolved id was refused: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(graph.Nodes))
	}
}

// cgIDs is comma-separated, and one bad id in a list must fail the whole call
// rather than silently building a node that half works.
func TestAddNodeChecksEveryIDInTheList(t *testing.T) {
	graph := &models.WorkflowGraph{}
	resolved := map[string]bool{"bitcoin": true}

	_, err := addGraphNodeResolved(graph, map[string]any{
		"type": "action", "template": "coingecko", "name": "Prices",
		"config": map[string]any{"cgIDs": "bitcoin,myriad"},
	}, resolved)
	if err == nil {
		t.Fatal("a list containing an unresolved id was accepted")
	}
	if !strings.Contains(err.Error(), "myriad") || strings.Contains(err.Error(), "bitcoin") {
		t.Errorf("the refusal should name only the unresolved id, got: %v", err)
	}
}

// Every other template is unaffected.
func TestAddNodeDoesNotCheckIDsOnOtherTemplates(t *testing.T) {
	graph := &models.WorkflowGraph{}
	if _, err := addGraphNodeResolved(graph, map[string]any{
		"type": "action", "template": "hackernews", "name": "HN",
	}, map[string]bool{}); err != nil {
		t.Fatalf("an unrelated template was refused: %v", err)
	}
}

// update_node gets the same check, and a refused update leaves the node as
// it was rather than half-applied.
func TestUpdateNodeRefusesAnUnresolvedCoinGeckoID(t *testing.T) {
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{{
		ID: "n1", Type: models.NodeTypeAction, Template: "coingecko", Name: "Price",
		Config: map[string]string{"cgIDs": "bitcoin"},
	}}}
	resolved := map[string]bool{"bitcoin": true}

	_, err := updateGraphNodeResolved(graph, map[string]any{
		"id": "n1", "name": "Renamed", "config": map[string]any{"cgIDs": "myriad"},
	}, resolved)
	if err == nil {
		t.Fatal("update_node accepted a guessed coin id")
	}
	if !strings.HasPrefix(err.Error(), "update_node:") || !strings.Contains(err.Error(), "resolve_coin") {
		t.Errorf("refusal = %v", err)
	}
	if n := graph.Nodes[0]; n.Config["cgIDs"] != "bitcoin" || n.Name != "Price" {
		t.Errorf("the refused update was applied anyway: %+v", n)
	}

	resolved["ethereum"] = true
	if _, err := updateGraphNodeResolved(graph, map[string]any{
		"id": "n1", "config": map[string]any{"cgIDs": "ethereum"},
	}, resolved); err != nil {
		t.Fatalf("a resolved id was refused on update: %v", err)
	}
}

// A coin node already on the canvas, its ids set by hand in the Inspector,
// must not become uneditable: an update that does not touch the ids is not
// the model guessing one.
func TestUpdateNodeLeavesIDsItDidNotTouchAlone(t *testing.T) {
	graph := &models.WorkflowGraph{Nodes: []models.WorkflowNode{{
		ID: "n1", Type: models.NodeTypeAction, Template: "coingecko", Name: "Price",
		Config: map[string]string{"cgIDs": "set-by-hand"},
	}}}
	if _, err := updateGraphNodeResolved(graph, map[string]any{
		"id": "n1", "name": "Renamed",
	}, map[string]bool{}); err != nil {
		t.Fatalf("a rename was refused over an id it did not set: %v", err)
	}
}

// The whole path the model takes: an add_node with a guessed id through the
// build dispatcher is refused, and succeeds once resolve_coin has returned it.
func TestBuildDispatcherEnforcesResolvedCoinIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"coins": []map[string]any{
			{"id": "algorand", "symbol": "algo", "name": "Algorand", "market_cap_rank": 50},
		}})
	}))
	defer srv.Close()
	SetCoinGeckoAPIBaseForTest(srv.URL)
	defer SetCoinGeckoAPIBaseForTest("")

	graph := &models.WorkflowGraph{}
	resolved := map[string]bool{}
	add := geminiFuncCall{name: "add_node", args: map[string]any{
		"type": "action", "template": "coingecko", "name": "Price",
		"config": map[string]any{"cgIDs": "algorand"},
	}}
	res := dispatchBuildCall(context.Background(), graph, add, "k", nil, map[string]string{}, resolved, &testTracker{})
	if text, _ := res["result"].(string); !strings.HasPrefix(text, "error: ") {
		t.Fatalf("an id nobody looked up was accepted: %q", text)
	}

	dispatchBuildCall(context.Background(), graph, geminiFuncCall{
		name: "resolve_coin", args: map[string]any{"query": "algo"},
	}, "k", nil, map[string]string{}, resolved, &testTracker{})

	res = dispatchBuildCall(context.Background(), graph, add, "k", nil, map[string]string{}, resolved, &testTracker{})
	if text, _ := res["result"].(string); strings.HasPrefix(text, "error: ") {
		t.Fatalf("a resolved id was refused: %q", text)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(graph.Nodes))
	}
}
