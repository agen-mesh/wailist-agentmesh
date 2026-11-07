package arkivshare

import (
	"context"
	"errors"
)

var (
	ErrDisabled    = errors.New("share disabled")
	ErrUnconfirmed = errors.New("share unconfirmed")
)

// Receipt is the chain result of one confirmed share.
type Receipt struct {
	EntityKey string
	TxHash    string
}

// Writer creates one Arkiv share entity. The payload is the POST body.
type Writer interface {
	CreateShare(ctx context.Context, payload []byte) (Receipt, error)
}
