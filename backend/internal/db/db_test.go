package db_test

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/testutil"
)

func TestConnect(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	baseURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	var tests = []struct {
		name   string
		scheme string
		params url.Values
	}{
		{name: "postgres", scheme: "postgres"},
		{name: "postgresql", scheme: "postgresql"},
		{
			name: "pool limits", scheme: "postgresql",
			params: url.Values{
				"pool_max_conns":      {"30"},
				"pool_min_conns":      {"0"},
				"pool_min_idle_conns": {"0"},
			},
		},
		{
			name: "pool timing and escaped connection option", scheme: "postgres",
			params: url.Values{
				"pool_max_conn_lifetime":        {"1m"},
				"pool_max_conn_lifetime_jitter": {"1s"},
				"pool_max_conn_idle_time":       {"30s"},
				"pool_health_check_period":      {"10s"},
				"pool_ping_timeout":             {"5s"},
				"application_name":              {"migration test: a&b + c?"},
				"default_query_exec_mode":       {"cache_statement"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connectionURL := *baseURL
			connectionURL.Scheme = test.scheme
			params := connectionURL.Query()
			for key, values := range test.params {
				params[key] = values
			}
			connectionURL.RawQuery = params.Encode()
			store, connectErr := db.New(context.Background(), connectionURL.String())
			if connectErr != nil {
				t.Fatalf("connect: %v", connectErr)
			}
			t.Cleanup(store.Close)
		})
	}
}

// testStore is shared by all db tests.
func testStore(t *testing.T) *db.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	store, err := db.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(store.Close)
	testutil.EnsureUsers(t, "dev")
	return store
}
