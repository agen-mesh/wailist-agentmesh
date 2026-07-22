package payments_test

import (
	"context"
	"errors"
	"testing"

	"github.com/agentmesh/backend/internal/payments"
)

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
