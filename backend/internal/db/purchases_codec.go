package db

import (
	"encoding/json"
	"fmt"

	"github.com/agentmesh/backend/internal/merchant"
	"github.com/agentmesh/backend/internal/purchase"
)

// JSONB normalizes embedded JSON, so byte commitments use base64 scalar fields.
type storedPurchase struct {
	purchase.Order
	StorageVersion int    `json:"storageVersion"`
	RequestBytes   []byte `json:"requestBytes"`
	ResultBytes    []byte `json:"resultBytes"`
}

type storedMerchant struct {
	merchant.Record
	StorageVersion       int    `json:"storageVersion"`
	CanonicalIntentBytes []byte `json:"canonicalIntentBytes"`
	RequestBytes         []byte `json:"requestBytes"`
	ResultBytes          []byte `json:"resultBytes"`
}

func checkOrderBytes(o purchase.Order) error {
	intent, err := json.Marshal(o.Intent)
	if err != nil || purchase.Digest(intent) != o.IntentHash {
		return fmt.Errorf("%w: stored intent commitment", purchase.ErrConflict)
	}
	if o.Intent.RequestDigest != "" && purchase.Digest(o.Request) != o.Intent.RequestDigest {
		return fmt.Errorf("%w: stored request commitment", purchase.ErrConflict)
	}
	if o.Intent.QuoteDigest != "" {
		quote, err := json.Marshal(o.Quote)
		if err != nil || purchase.Digest(quote) != o.Intent.QuoteDigest {
			return fmt.Errorf("%w: stored quote commitment", purchase.ErrConflict)
		}
	}
	return nil
}

func encodeOrder(o purchase.Order) ([]byte, error) {
	if err := checkOrderBytes(o); err != nil {
		return nil, err
	}
	return json.Marshal(storedPurchase{Order: o, StorageVersion: 1, RequestBytes: o.Request, ResultBytes: o.Result})
}

func restoreOrder(raw []byte) (purchase.Order, error) {
	var stored storedPurchase
	if err := json.Unmarshal(raw, &stored); err != nil {
		return purchase.Order{}, err
	}
	o := stored.Order
	switch stored.StorageVersion {
	case 1:
		o.Request, o.Result = stored.RequestBytes, stored.ResultBytes
	case 0:
		if len(o.Result) > 0 && string(o.Result) != "null" {
			return o, fmt.Errorf("%w: legacy result requires reconciliation", purchase.ErrConflict)
		}
		if o.Intent.RequestDigest != "" {
			var err error
			o.Request, err = purchase.CanonicalRequest(o.Request)
			if err != nil {
				return o, fmt.Errorf("%w: legacy request encoding", purchase.ErrConflict)
			}
		}
	default:
		return o, fmt.Errorf("%w: stored purchase version", purchase.ErrConflict)
	}
	return o, checkOrderBytes(o)
}

func checkMerchantBytes(r merchant.Record) error {
	s := r.Snapshot
	intent, err := merchant.DecodeIntent(s.CanonicalIntent)
	if err != nil || merchant.Digest(s.CanonicalIntent) != s.IntentHash || merchant.Digest(s.Request) != s.RequestDigest || intent.RequestDigest != s.RequestDigest || intent.QuoteDigest != s.QuoteDigest {
		return fmt.Errorf("%w: stored merchant commitment", merchant.ErrConflict)
	}
	return nil
}

func encodeMerchant(r merchant.Record) ([]byte, error) {
	if err := checkMerchantBytes(r); err != nil {
		return nil, err
	}
	return json.Marshal(storedMerchant{Record: r, StorageVersion: 1, CanonicalIntentBytes: r.Snapshot.CanonicalIntent, RequestBytes: r.Snapshot.Request, ResultBytes: r.Result})
}

func restoreMerchant(raw []byte) (merchant.Record, error) {
	var stored storedMerchant
	if err := json.Unmarshal(raw, &stored); err != nil {
		return merchant.Record{}, err
	}
	r := stored.Record
	switch stored.StorageVersion {
	case 1:
		r.Snapshot.CanonicalIntent, r.Snapshot.Request, r.Result = stored.CanonicalIntentBytes, stored.RequestBytes, stored.ResultBytes
	case 0:
		if len(r.Result) > 0 && string(r.Result) != "null" {
			return r, fmt.Errorf("%w: legacy result requires reconciliation", merchant.ErrConflict)
		}
		var intent merchant.Intent
		if err := json.Unmarshal(r.Snapshot.CanonicalIntent, &intent); err != nil {
			return r, fmt.Errorf("%w: legacy intent encoding", merchant.ErrConflict)
		}
		var err error
		r.Snapshot.CanonicalIntent, err = json.Marshal(intent)
		if err != nil {
			return r, err
		}
		r.Snapshot.Request, err = purchase.CanonicalRequest(r.Snapshot.Request)
		if err != nil {
			return r, fmt.Errorf("%w: legacy merchant request encoding", merchant.ErrConflict)
		}
	default:
		return r, fmt.Errorf("%w: stored merchant version", merchant.ErrConflict)
	}
	return r, checkMerchantBytes(r)
}
