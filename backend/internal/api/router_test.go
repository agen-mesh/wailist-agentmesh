package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/api"
	"github.com/agentmesh/backend/internal/api/handlers"
)

func TestHealthCheck(t *testing.T) {
	r := api.NewRouter(&handlers.Deps{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
}

// Pins where the read-only middleware actually sits in the routing.
//
// It is registered inside the JWT group, and a chi group's middleware runs only
// for paths the router matched. readonly.go normalises a trailing slash
// defensively; this documents that the normalisation is unreachable today,
// because chi answers 404 for the trailing-slash form long before the group is
// entered. blocksWrite's own unit tests call it directly and so cannot show
// this -- which is exactly the gap this closes.
//
// If slash-handling middleware (StripSlashes/RedirectSlashes) is ever added to
// NewRouter, the 404s below turn into 401s and this test fails loudly. That is
// the moment the defensive normalisation starts earning its keep, and the
// failure is the signal to go re-read it.
// The run history routes list what a user's workflows did and spent, so they
// must sit behind the session like every other workflow route.
func TestRunHistoryRoutesRequireASession(t *testing.T) {
	r := api.NewRouter(&handlers.Deps{})
	for _, path := range []string{"/runs", "/workflows/wf_1/runs"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("GET %s without a session = %d, want 401", path, w.Code)
			}
		})
	}
}

func TestTrailingSlashNeverReachesTheAuthedGroup(t *testing.T) {
	r := api.NewRouter(&handlers.Deps{})

	cases := []struct {
		name   string
		method string
		path   string
		want   int
		why    string
	}{
		{"collection matches", http.MethodPost, "/workflows", http.StatusUnauthorized,
			"route matched, so the group's auth middleware ran and rejected it"},
		{"collection with slash", http.MethodPost, "/workflows/", http.StatusNotFound,
			"no route matched, so no group middleware ran at all"},
		{"item matches", http.MethodPut, "/workflows/wf_1", http.StatusUnauthorized,
			"route matched, so the group's auth middleware ran and rejected it"},
		{"item with slash", http.MethodPut, "/workflows/wf_1/", http.StatusNotFound,
			"no route matched, so no group middleware ran at all"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, tc.why)
			}
		})
	}
}

// Read-only mode judges the path the way chi routes it. An escaped separator in
// a variable key used to reach the handler because the middleware looked only
// at the decoded path, which has one segment more and matched no rule.
func TestReadOnlyBlocksEscapedVariableKeys(t *testing.T) {
	t.Setenv("WEB_READONLY_MODE", "1")
	const secret = "test-secret"
	r := api.NewRouter(&handlers.Deps{JWTSecret: secret})
	token := api.TestMakeToken(secret, "user-1")
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, path := range []string{
			"/workflows/wf_1/variables/API_KEY",
			"/workflows/wf_1/variables/API%2FKEY",
		} {
			t.Run(method+" "+path, func(t *testing.T) {
				req := httptest.NewRequest(method, path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("%s %s = %d, want %d", method, path, w.Code, http.StatusForbidden)
				}
			})
		}
	}
}
