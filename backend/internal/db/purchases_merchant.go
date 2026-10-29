package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/merchant"
	"github.com/jackc/pgx/v5"
)

type MerchantStore struct{ store *Store }

func (s *Store) MerchantPurchases() *MerchantStore { return &MerchantStore{store: s} }

var _ merchant.Store = (*MerchantStore)(nil)

func merchantRecord(raw []byte, err error) (merchant.Record, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return merchant.Record{}, merchant.ErrNotFound
	}
	if err != nil {
		return merchant.Record{}, err
	}
	return restoreMerchant(raw)
}
func (m *MerchantStore) Get(ctx context.Context, id string) (merchant.Record, error) {
	var raw []byte
	err := m.store.pool.QueryRow(ctx, `SELECT payload FROM purchase_merchant_orders WHERE order_id=$1`, id).Scan(&raw)
	return merchantRecord(raw, err)
}
func (m *MerchantStore) Accept(ctx context.Context, s merchant.Snapshot) (merchant.Record, error) {
	r := merchant.Record{Snapshot: s, State: merchant.Accepted, AcceptedAt: time.Now().UTC()}
	raw, err := encodeMerchant(r)
	if err != nil {
		return r, err
	}
	if _, err = m.store.pool.Exec(ctx, `INSERT INTO purchase_merchant_orders(order_id,intent_hash,payload,state) VALUES($1,$2,$3,$4) ON CONFLICT(order_id) DO NOTHING`, s.OrderID, s.IntentHash, string(raw), r.State); err != nil {
		return r, err
	}
	stored, err := m.Get(ctx, s.OrderID)
	if err != nil {
		return stored, err
	}
	a, _ := json.Marshal(stored.Snapshot)
	b, _ := json.Marshal(s)
	if string(a) != string(b) {
		return stored, merchant.ErrConflict
	}
	return stored, nil
}
func (m *MerchantStore) Claim(ctx context.Context, id, intent string, event merchant.PaymentEvent) (merchant.Record, bool, error) {
	tx, err := m.store.pool.Begin(ctx)
	if err != nil {
		return merchant.Record{}, false, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM purchase_merchant_orders WHERE order_id=$1 FOR UPDATE`, id).Scan(&raw)
	r, err := merchantRecord(raw, err)
	if err != nil {
		return r, false, err
	}
	if r.Snapshot.IntentHash != intent {
		return r, false, merchant.ErrConflict
	}
	if r.State != merchant.Accepted && r.State != merchant.Pending {
		if r.Payment != nil && *r.Payment != event {
			return r, false, merchant.ErrConflict
		}
		return r, false, nil
	}
	index, err := strconv.ParseInt(event.LogIndex, 10, 64)
	if err != nil || index < 0 {
		return r, false, merchant.ErrPayment
	}
	if _, err = tx.Exec(ctx, `INSERT INTO purchase_merchant_events(chain_id,tx_hash,log_index,order_id) VALUES($1,$2,$3,$4)`, event.ChainID, strings.ToLower(event.TxHash), index, id); err != nil {
		return r, false, fmt.Errorf("merchant event claim: %w", err)
	}
	r.State = merchant.Started
	r.Payment = &event
	raw, err = encodeMerchant(r)
	if err != nil {
		return r, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_merchant_orders SET payload=$1,state=$2,updated_at=now() WHERE order_id=$3`, string(raw), r.State, id); err != nil {
		return r, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, false, err
	}
	return r, true, nil
}
func (m *MerchantStore) Finish(ctx context.Context, id, intent string, out merchant.Outcome) (merchant.Record, error) {
	tx, err := m.store.pool.Begin(ctx)
	if err != nil {
		return merchant.Record{}, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM purchase_merchant_orders WHERE order_id=$1 FOR UPDATE`, id).Scan(&raw)
	r, err := merchantRecord(raw, err)
	if err != nil {
		return r, err
	}
	if r.Snapshot.IntentHash != intent {
		return r, merchant.ErrConflict
	}
	if r.State == merchant.Completed || r.State == merchant.Failed || r.State == merchant.Unknown {
		return r, nil
	}
	if r.State != merchant.Started || (out.State != merchant.Completed && out.State != merchant.Failed && out.State != merchant.Unknown) {
		return r, merchant.ErrConflict
	}
	r.State = out.State
	r.Result = out.Result
	r.FailureCode = out.FailureCode
	raw, err = encodeMerchant(r)
	if err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_merchant_orders SET payload=$1,state=$2,updated_at=now() WHERE order_id=$3`, string(raw), r.State, id); err != nil {
		return r, err
	}
	if err = tx.Commit(ctx); err != nil {
		return r, err
	}
	return r, nil
}
