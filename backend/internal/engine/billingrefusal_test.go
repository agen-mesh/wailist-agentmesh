package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine/nodes"
)

// A degraded run reports success and answers with what it has. That is the
// right outcome for a source that is down and only for that: anything the
// billing gate produced must fail the run instead, or a workflow silently
// stops doing the paid half of its job and never says why.
func TestIsBillingRefusal(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "not enough credits, wrapped the way preflightCheck wraps it",
			err:  fmt.Errorf("insufficient credits: balance %d micros, need %d micros: %w", 0, 500_000, db.ErrInsufficientCredits),
			want: true,
		},
		{
			// The gate could not reach a verdict. Nothing established that
			// the work was affordable, so nothing may proceed as if it were.
			name: "the balance lookup itself failed",
			err:  fmt.Errorf("checking credit balance: %w", errBillingCheckFailed),
			want: true,
		},
		{
			name: "an attached tool call blocked on balance",
			err:  fmt.Errorf("tool: %w", &nodes.ErrBalanceBlocked{}),
			want: true,
		},
		{
			name: "the node's own work failing is not a billing refusal",
			err:  errors.New("http: GET 503"),
			want: false,
		},
		{
			name: "a source timing out is not a billing refusal",
			err:  fmt.Errorf("coingecko: %w", errors.New("context deadline exceeded")),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBillingRefusal(tt.err); got != tt.want {
				t.Errorf("isBillingRefusal(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
