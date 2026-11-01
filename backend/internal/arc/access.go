package arc

import (
	"context"
	"errors"
)

var ErrOperatorRequired = errors.New("Arc shared wallet requires an authorized operator")

var ErrSubmissionUncertain = errors.New("Arc submission requires reconciliation")

type principalKey struct{}

type workflowOwnerKey struct{}

// WithPrincipal accepts only an account ID established by server authentication.
func WithPrincipal(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, principalKey{}, accountID)
}

// DetachedPrincipal preserves authentication for a run without retaining request cancellation or payload values.
func DetachedPrincipal(ctx context.Context) context.Context {
	accountID, _ := ctx.Value(principalKey{}).(string)
	return WithPrincipal(context.Background(), accountID)
}

// AuthorizeWorkflowOwner detaches the request and lets the workflow owner
// broadcast an Arc node inside that workflow. Scheduler, geofence, and
// webhook runs have no request principal. A different authenticated
// principal is not granted the owner's spend. HTTP handlers must not call
// this; they keep the operator allowlist.
func AuthorizeWorkflowOwner(requestCtx context.Context, ownerID string) context.Context {
	ctx := DetachedPrincipal(requestCtx)
	principal, _ := ctx.Value(principalKey{}).(string)
	if ownerID == "" || (principal != "" && principal != ownerID) {
		return ctx
	}
	ctx = context.WithValue(ctx, workflowOwnerKey{}, ownerID)
	if principal == "" {
		ctx = WithPrincipal(ctx, ownerID)
	}
	return ctx
}

func (c Config) requireOperator(ctx context.Context) error {
	accountID, _ := ctx.Value(principalKey{}).(string)
	ownerID, _ := ctx.Value(workflowOwnerKey{}).(string)
	if accountID != "" && accountID == ownerID {
		return nil
	}
	if accountID != "" {
		if _, ok := c.operatorUserIDs[accountID]; ok {
			return nil
		}
	}
	return ErrOperatorRequired
}
