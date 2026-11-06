package arkivshare

import (
	"context"
	"errors"
)

var (
	ErrDisabled    = errors.New("share disabled")
	ErrUnconfirmed = errors.New("share unconfirmed")
)

// Writer creates one Arkiv share entity. The payload is the POST body.
type Writer interface {
	CreateShare(ctx context.Context, payload []byte) (entityKey string, err error)
}
