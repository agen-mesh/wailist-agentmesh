package payments_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentmesh/backend/internal/payments"
)

func fxTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func TestFetchINRToUSDRateUsesOverride(t *testing.T) {
	payments.SetFetchRateForTest(func(context.Context) (float64, error) {
		return 0.012, nil
	})
	defer payments.SetFetchRateForTest(nil)

	rate, err := payments.FetchINRToUSDRate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rate != 0.012 {
		t.Fatalf("want 0.012 got %v", rate)
	}
}

func TestLiveFetchINRToUSDRejectsRateBelowPlausibleBounds(t *testing.T) {
	srv := fxTestServer(t, `{"rates":{"USD":0.0001}}`)
	defer srv.Close()

	_, err := payments.LiveFetchINRToUSDForTest(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("want error for implausibly low rate, got nil")
	}
}

func TestLiveFetchINRToUSDRejectsRateAbovePlausibleBounds(t *testing.T) {
	srv := fxTestServer(t, `{"rates":{"USD":1.0}}`)
	defer srv.Close()

	_, err := payments.LiveFetchINRToUSDForTest(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("want error for implausibly high rate, got nil")
	}
}

func TestLiveFetchINRToUSDAcceptsPlausibleRate(t *testing.T) {
	srv := fxTestServer(t, `{"rates":{"USD":0.012}}`)
	defer srv.Close()

	rate, err := payments.LiveFetchINRToUSDForTest(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 0.012 {
		t.Fatalf("want 0.012 got %v", rate)
	}
}

func TestFetchINRToUSDRatePropagatesError(t *testing.T) {
	payments.SetFetchRateForTest(func(context.Context) (float64, error) {
		return 0, errors.New("fx api down")
	})
	defer payments.SetFetchRateForTest(nil)

	_, err := payments.FetchINRToUSDRate(context.Background())
	if err == nil {
		t.Fatal("want error, got nil")
	}
}
