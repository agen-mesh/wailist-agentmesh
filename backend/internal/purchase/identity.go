package purchase

import (
	"context"
	"net/url"

	"github.com/agentmesh/backend/internal/world"
)

type IdentityBindings interface {
	Binding(context.Context, string, string, string, string) (world.Binding, error)
	Resolve(context.Context, string) (world.Binding, error)
}
type WorldIdentity struct {
	Client           *world.Client
	Store            IdentityBindings
	Issuer           string
	AuthorizationURL func(string) (string, error)
}

func (w *WorldIdentity) Begin(ctx context.Context, o Order) (IdentityStart, error) {
	b, err := w.Store.Binding(ctx, o.AccountID, o.ID, o.IntentHash, w.Issuer)
	if err != nil {
		return IdentityStart{}, err
	}
	a, err := w.Client.Begin(ctx, b)
	if err != nil {
		return IdentityStart{}, err
	}
	if w.AuthorizationURL != nil {
		a.AuthorizationURL, err = w.AuthorizationURL(a.AuthorizationURL)
		if err != nil {
			return IdentityStart{}, err
		}
	}
	return IdentityStart{AuthorizationURL: a.AuthorizationURL, ExpiresAt: a.ExpiresAt}, nil
}
func (w *WorldIdentity) Callback(ctx context.Context, account string, query url.Values) (string, error) {
	if account == "" || len(query["state"]) != 1 {
		return "", ErrDenied
	}
	b, err := w.Store.Resolve(ctx, query.Get("state"))
	if err != nil || b.AccountID != account {
		return "", ErrDenied
	}
	_, err = w.Client.Callback(ctx, b, query)
	if err != nil {
		return "", ErrDenied
	}
	return b.OrderID, nil
}
