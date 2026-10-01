package nodes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearchModelDefaultAndOverride(t *testing.T) {
	t.Cleanup(func() { SetWebSearchModel("") })

	SetWebSearchModel("")
	if got := webSearchModelID(); got != defaultWebSearchModel {
		t.Errorf("unset = %q, want the default %q", got, defaultWebSearchModel)
	}
	// Pinned deliberately. Moving the default to a 3.x model is cheaper per
	// grounded search, but no one has yet confirmed that model grounds on
	// this account; a model that rejects grounding breaks every web search.
	// Change this only alongside a live check -- see WEB_SEARCH_MODEL in
	// backend/.env.example.
	if defaultWebSearchModel != "gemini-2.5-flash" {
		t.Errorf("default moved to %q without a live grounding check", defaultWebSearchModel)
	}

	SetWebSearchModel("  gemini-3.5-flash  ")
	if got := webSearchModelID(); got != "gemini-3.5-flash" {
		t.Errorf("override = %q, want %q (trimmed)", got, "gemini-3.5-flash")
	}

	SetWebSearchModel("")
	if got := webSearchModelID(); got != defaultWebSearchModel {
		t.Errorf("after clearing = %q, want the default %q", got, defaultWebSearchModel)
	}
}

// TestWebSearchCallsTheConfiguredModel checks the wiring, not just the
// getter: the model has to reach the request URL.
func TestWebSearchCallsTheConfiguredModel(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"an answer"}]}}]}`))
	}))
	defer srv.Close()

	oldBase := geminiBaseURL
	geminiBaseURL = srv.URL
	t.Cleanup(func() {
		geminiBaseURL = oldBase
		SetWebSearchModel("")
	})

	SetWebSearchModel("gemini-3.5-flash")
	if _, err := webSearch(context.Background(), "bitcoin price", "k"); err != nil {
		t.Fatalf("webSearch: %v", err)
	}
	if !strings.Contains(path, "/models/gemini-3.5-flash:generateContent") {
		t.Errorf("request path = %q, want it to name the configured model", path)
	}
}
