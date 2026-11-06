package arc

import (
	"context"
	"errors"
)

var ErrOperatorRequired = errors.New("Arc shared wallet requires an authorized operator")

var ErrSubmissionUncertain = errors.New("Arc submission requires reconciliation")

type principalKey struct{}

// WithPrincipal accepts only an account ID established by server authentication.
func WithPrincipal(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, principalKey{}, accountID)
}

// DetachedPrincipal preserves authentication for a run without retaining request cancellation or payload values.
func DetachedPrincipal(ctx context.Context) context.Context {
	accountID, _ := ctx.Value(principalKey{}).(string)
	return WithPrincipal(context.Background(), accountID)
}

func (c Config) requireOperator(ctx context.Context) error {
	accountID, _ := ctx.Value(principalKey{}).(string)
	if accountID != "" {
		if _, ok := c.operatorUserIDs[accountID]; ok {
			return nil
		}
	}
	return ErrOperatorRequired
}
