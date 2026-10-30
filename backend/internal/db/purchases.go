package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/purchase"
	"github.com/jackc/pgx/v5"
)

type PurchaseStore struct{ store *Store }

func (s *Store) Purchases() *PurchaseStore { return &PurchaseStore{store: s} }

var _ purchase.Store = (*PurchaseStore)(nil)

func decodeOrder(raw []byte, err error) (purchase.Order, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return purchase.Order{}, purchase.ErrNotFound
	}
	if err != nil {
		return purchase.Order{}, fmt.Errorf("purchase journal read: %w", err)
	}
	o, err := restoreOrder(raw)
	if err != nil {
		return o, fmt.Errorf("purchase journal decode: %w", err)
	}
	return o, nil
}
func (p *PurchaseStore) Get(ctx context.Context, account, id string) (purchase.Order, error) {
	var raw []byte
	err := p.store.pool.QueryRow(ctx, `SELECT payload FROM purchase_orders WHERE account_id=$1 AND id=$2`, account, id).Scan(&raw)
	return decodeOrder(raw, err)
}
func (p *PurchaseStore) ByRequest(ctx context.Context, account, request string) (purchase.Order, error) {
	var raw []byte
	err := p.store.pool.QueryRow(ctx, `SELECT payload FROM purchase_orders WHERE account_id=$1 AND client_request_id=$2`, account, request).Scan(&raw)
	return decodeOrder(raw, err)
}
func (p *PurchaseStore) Create(ctx context.Context, o purchase.Order) (purchase.Order, bool, error) {
	raw, err := encodeOrder(o)
	if err != nil {
		return o, false, err
	}
	tag, err := p.store.pool.Exec(ctx, `INSERT INTO purchase_orders(id,account_id,client_request_id,intent_hash,payer,chain_id,asset,run_id,node_id,invocation,state,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(account_id,client_request_id) DO NOTHING`, o.ID, o.AccountID, o.ClientRequestID, o.IntentHash, o.Intent.Payer, o.Intent.ChainID, o.Intent.Asset, o.Intent.RunID, o.Intent.NodeID, o.Intent.Invocation, o.State, string(raw))
	if err != nil {
		return o, false, fmt.Errorf("insert purchase: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return o, true, nil
	}
	existing, err := p.ByRequest(ctx, o.AccountID, o.ClientRequestID)
	return existing, false, err
}

func (p *PurchaseStore) Update(ctx context.Context, account, id string, fn func(*purchase.Order, purchase.Ledger) error) (purchase.Order, error) {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return purchase.Order{}, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM purchase_orders WHERE account_id=$1 AND id=$2 FOR UPDATE`, account, id).Scan(&raw)
	o, err := decodeOrder(raw, err)
	if err != nil {
		return o, err
	}
	before, _ := json.Marshal(o.Intent)
	l := &purchaseLedger{ctx: ctx, tx: tx, account: account, wallet: o.Intent.Payer, orderID: id}
	if err = fn(&o, l); err != nil {
		return o, err
	}
	after, err := json.Marshal(o.Intent)
	if err != nil {
		return o, err
	}
	if string(before) != string(after) || o.ID != id || o.AccountID != account || purchase.Digest(after) != o.IntentHash {
		return o, purchase.ErrConflict
	}
	raw, err = encodeOrder(o)
	if err != nil {
		return o, err
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_orders SET payload=$1,state=$2,updated_at=now() WHERE id=$3 AND account_id=$4`, string(raw), o.State, id, account); err != nil {
		return o, fmt.Errorf("save purchase journal: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return o, fmt.Errorf("commit purchase journal: %w", err)
	}
	return o, nil
}

type purchaseLedger struct {
	ctx                      context.Context
	tx                       pgx.Tx
	account, wallet, orderID string
}

func (l *purchaseLedger) Reserve(amount string) error {
	if _, err := purchase.Amount(amount); err != nil {
		return err
	}
	tag, err := l.tx.Exec(l.ctx, `UPDATE purchase_allowances SET reserved_micros=reserved_micros+$1 WHERE account_id=$2 AND wallet=$3 AND limit_micros::numeric-reserved_micros-consumed_micros >= $1::numeric AND EXISTS(SELECT 1 FROM purchase_wallets WHERE account_id=$2 AND address=$3)`, amount, l.account, l.wallet)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("purchase allowance is insufficient or wallet changed")
	}
	return nil
}
func (l *purchaseLedger) Consume(amount string) error {
	tag, err := l.tx.Exec(l.ctx, `UPDATE purchase_allowances SET reserved_micros=reserved_micros-$1,consumed_micros=consumed_micros+$1 WHERE account_id=$2 AND wallet=$3 AND reserved_micros >= $1`, amount, l.account, l.wallet)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return purchase.ErrConflict
	}
	return nil
}
func (l *purchaseLedger) Release(amount string) error {
	tag, err := l.tx.Exec(l.ctx, `UPDATE purchase_allowances SET reserved_micros=reserved_micros-$1 WHERE account_id=$2 AND wallet=$3 AND reserved_micros >= $1`, amount, l.account, l.wallet)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return purchase.ErrConflict
	}
	return nil
}
func (l *purchaseLedger) BindEvent(e purchase.PaymentEvent) error {
	if e.ChainID != purchase.ChainID {
		return purchase.ErrDenied
	}
	index, err := strconv.ParseInt(e.LogIndex, 10, 64)
	if err != nil || index < 0 {
		return purchase.ErrDenied
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = l.tx.Exec(l.ctx, `INSERT INTO purchase_payment_events(chain_id,tx_hash,log_index,order_id,payload) VALUES($1,$2,$3,$4,$5)`, e.ChainID, strings.ToLower(e.TxHash), index, l.orderID, string(raw))
	if err != nil {
		return fmt.Errorf("payment event already bound or unavailable: %w", err)
	}
	return nil
}
func (l *purchaseLedger) RelayNonce(relayer string, observed uint64) (uint64, error) {
	if observed >= math.MaxInt64 {
		return 0, purchase.ErrDenied
	}
	if _, err := purchase.Address(relayer); err != nil {
		return 0, err
	}
	if _, err := l.tx.Exec(l.ctx, `INSERT INTO purchase_relayer_nonces(chain_id,relayer,next_nonce) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, purchase.ChainID, relayer, int64(observed)); err != nil {
		return 0, err
	}
	var next int64
	if err := l.tx.QueryRow(l.ctx, `SELECT next_nonce FROM purchase_relayer_nonces WHERE chain_id=$1 AND relayer=$2 FOR UPDATE`, purchase.ChainID, relayer).Scan(&next); err != nil {
		return 0, err
	}
	if uint64(next) < observed {
		next = int64(observed)
	}
	if uint64(next) > observed {
		return 0, purchase.ErrUncertain
	}
	if next == math.MaxInt64 {
		return 0, purchase.ErrDenied
	}
	_, err := l.tx.Exec(l.ctx, `UPDATE purchase_relayer_nonces SET next_nonce=$3 WHERE chain_id=$1 AND relayer=$2`, purchase.ChainID, relayer, next+1)
	return uint64(next), err
}

func (p *PurchaseStore) Wallet(ctx context.Context, account string) (*purchase.Wallet, error) {
	w := new(purchase.Wallet)
	err := p.store.pool.QueryRow(ctx, `SELECT address,verified_at FROM purchase_wallets WHERE account_id=$1`, account).Scan(&w.Address, &w.VerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return w, nil
}
func (p *PurchaseStore) SaveChallenge(ctx context.Context, c purchase.WalletChallenge) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = p.store.pool.Exec(ctx, `INSERT INTO purchase_wallet_challenges(id,account_id,payload,expires_at) VALUES($1,$2,$3,$4)`, c.ID, c.AccountID, string(raw), c.ExpiresAt)
	return err
}
func (p *PurchaseStore) LinkWallet(ctx context.Context, account, id string, verify func(purchase.WalletChallenge) error) (purchase.Wallet, error) {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return purchase.Wallet{}, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM purchase_wallet_challenges WHERE id=$1 AND account_id=$2 AND consumed_at IS NULL AND expires_at > now() FOR UPDATE`, id, account).Scan(&raw)
	if err != nil {
		return purchase.Wallet{}, purchase.ErrDenied
	}
	var c purchase.WalletChallenge
	if err = json.Unmarshal(raw, &c); err != nil {
		return purchase.Wallet{}, err
	}
	if c.AccountID != account {
		return purchase.Wallet{}, purchase.ErrDenied
	}
	if err = verify(c); err != nil {
		return purchase.Wallet{}, err
	}
	w := purchase.Wallet{Address: c.Address, VerifiedAt: time.Now().UTC()}
	if _, err = tx.Exec(ctx, `INSERT INTO purchase_wallets(account_id,address,verified_at) VALUES($1,$2,$3) ON CONFLICT(account_id) DO UPDATE SET address=EXCLUDED.address,verified_at=EXCLUDED.verified_at`, account, w.Address, w.VerifiedAt); err != nil {
		return purchase.Wallet{}, fmt.Errorf("wallet belongs to another account or cannot be linked: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_wallet_challenges SET consumed_at=now() WHERE id=$1`, id); err != nil {
		return purchase.Wallet{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return purchase.Wallet{}, err
	}
	return w, nil
}
func (p *PurchaseStore) Allowance(ctx context.Context, account, wallet string) (purchase.Allowance, error) {
	a := purchase.Allowance{Wallet: wallet, ChainID: purchase.ChainID, Asset: purchase.Asset, LimitMicros: "0", ReservedMicros: "0", ConsumedMicros: "0", AvailableMicros: "0"}
	err := p.store.pool.QueryRow(ctx, `SELECT limit_micros::text,reserved_micros::text,consumed_micros::text,(limit_micros-reserved_micros-consumed_micros)::text FROM purchase_allowances WHERE account_id=$1 AND wallet=$2`, account, wallet).Scan(&a.LimitMicros, &a.ReservedMicros, &a.ConsumedMicros, &a.AvailableMicros)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, nil
	}
	return a, err
}
func (p *PurchaseStore) SetAllowance(ctx context.Context, account, wallet, limit string) (purchase.Allowance, error) {
	if _, err := purchase.Amount(limit); err != nil {
		return purchase.Allowance{}, err
	}
	tag, err := p.store.pool.Exec(ctx, `INSERT INTO purchase_allowances(account_id,wallet,limit_micros) SELECT $1,$2,$3 WHERE EXISTS(SELECT 1 FROM purchase_wallets WHERE account_id=$1 AND address=$2) ON CONFLICT(account_id,wallet) DO UPDATE SET limit_micros=EXCLUDED.limit_micros WHERE purchase_allowances.reserved_micros::numeric+purchase_allowances.consumed_micros <= EXCLUDED.limit_micros`, account, wallet, limit)
	if err != nil {
		return purchase.Allowance{}, err
	}
	if tag.RowsAffected() != 1 {
		return purchase.Allowance{}, purchase.ErrConflict
	}
	return p.Allowance(ctx, account, wallet)
}
