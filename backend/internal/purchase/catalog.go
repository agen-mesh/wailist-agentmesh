package purchase

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/agentmesh/backend/internal/arkiv"
)

type ArkivCatalog struct{ Client *arkiv.Client }

func offerFromArkiv(s arkiv.OfferSnapshot) Offer {
	schema, _ := json.Marshal(s.Payload.InputSchema)
	source, _ := json.Marshal(s)
	return Offer{Key: s.EntityKey, Owner: s.Owner, Version: strconv.FormatUint(s.Version, 10), ContentHash: s.ContentHash, SnapshotHash: s.SnapshotHash, Endpoint: s.Payload.Endpoint, Method: s.Payload.Method, Capability: s.Capability, InputSchema: schema, ChainID: int64(s.SettlementChain), Asset: s.Asset, Payee: s.Payload.Payee, PriceMicros: strconv.FormatUint(s.PriceMicros, 10), ValidUntil: time.Unix(int64(s.ValidUntil), 0).UTC(), ExpiresAtBlock: strconv.FormatUint(s.ExpiresAtBlock, 10), ObservedBlock: strconv.FormatUint(s.ObservedAtBlock, 10), Source: source}
}
func (a ArkivCatalog) List(ctx context.Context, capability, max string) ([]Offer, error) {
	q := arkiv.QueryOptions{Capability: capability, Limit: 50}
	if max != "" {
		v, err := strconv.ParseUint(max, 10, 64)
		if err != nil {
			return nil, err
		}
		q.MaxPriceMicros = v
	}
	page, err := a.Client.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]Offer, 0, len(page.Offers))
	for _, s := range page.Offers {
		if _, err := Amount(strconv.FormatUint(s.PriceMicros, 10)); err == nil {
			out = append(out, offerFromArkiv(s))
		}
	}
	return out, nil
}
func (a ArkivCatalog) Get(ctx context.Context, key string) (Offer, error) {
	s, err := a.Client.Get(ctx, key)
	if err != nil {
		return Offer{}, err
	}
	return offerFromArkiv(s), nil
}
func (a ArkivCatalog) Revalidate(ctx context.Context, o Offer) error {
	var s arkiv.OfferSnapshot
	if err := json.Unmarshal(o.Source, &s); err != nil {
		return err
	}
	current, err := a.Client.Revalidate(ctx, s)
	if err != nil {
		return err
	}
	if current.SnapshotHash != o.SnapshotHash {
		return ErrConflict
	}
	return nil
}
