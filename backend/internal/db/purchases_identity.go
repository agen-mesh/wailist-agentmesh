package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/wallet"
	"github.com/agentmesh/backend/internal/world"
	"github.com/jackc/pgx/v5"
)

type PurchaseIdentityStore struct {
	store *Store
	key   string
}

func (s *Store) PurchaseIdentities(key string) *PurchaseIdentityStore {
	return &PurchaseIdentityStore{store: s, key: key}
}

var _ world.Store = (*PurchaseIdentityStore)(nil)

type privateAttempt struct {
	Attempt                  world.Attempt
	Nonce, Verifier, Subject string
}

func (p *PurchaseIdentityStore) seal(a world.Attempt) (string, error) {
	raw, err := json.Marshal(privateAttempt{a, a.Nonce, a.CodeVerifier, a.Binding.ExpectedSubject})
	if err != nil {
		return "", err
	}
	return wallet.Encrypt(string(raw), p.key)
}
func (p *PurchaseIdentityStore) open(sealed string) (world.Attempt, error) {
	raw, err := wallet.Decrypt(sealed, p.key)
	if err != nil {
		return world.Attempt{}, err
	}
	var a privateAttempt
	if err = json.Unmarshal([]byte(raw), &a); err != nil {
		return world.Attempt{}, err
	}
	a.Attempt.Nonce = a.Nonce
	a.Attempt.CodeVerifier = a.Verifier
	a.Attempt.Binding.ExpectedSubject = a.Subject
	return a.Attempt, nil
}
func (p *PurchaseIdentityStore) Create(ctx context.Context, a world.Attempt) error {
	sealed, err := p.seal(a)
	if err != nil {
		return err
	}
	tag, err := p.store.pool.Exec(ctx, `INSERT INTO purchase_identity_attempts(state_hash,id,account_id,order_id,intent_hash,sealed_payload,expires_at) SELECT $1,$2,$3,$4,$5,$6,$7 WHERE EXISTS(SELECT 1 FROM purchase_orders WHERE id=$4 AND account_id=$3 AND intent_hash=$5 AND state IN ('awaiting_identity','awaiting_approval'))`, a.StateHash, a.ID, a.Binding.AccountID, a.Binding.OrderID, a.Binding.IntentHash, sealed, a.ExpiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return world.ErrAttemptUnavailable
	}
	return nil
}
func (p *PurchaseIdentityStore) Resolve(ctx context.Context, state string) (world.Binding, error) {
	hash, err := world.HashState(state)
	if err != nil {
		return world.Binding{}, world.ErrAttemptUnavailable
	}
	var sealed string
	err = p.store.pool.QueryRow(ctx, `SELECT sealed_payload FROM purchase_identity_attempts WHERE state_hash=$1 AND consumed_at IS NULL AND cancelled_at IS NULL AND expires_at>now()`, hash).Scan(&sealed)
	if err != nil {
		return world.Binding{}, world.ErrAttemptUnavailable
	}
	a, err := p.open(sealed)
	if err != nil {
		return world.Binding{}, world.ErrAttemptUnavailable
	}
	return a.Binding, nil
}
func (p *PurchaseIdentityStore) Binding(ctx context.Context, account, order, intent, issuer string) (world.Binding, error) {
	b := world.Binding{AccountID: account, OrderID: order, IntentHash: intent}
	var sealed string
	err := p.store.pool.QueryRow(ctx, `SELECT sealed_subject FROM purchase_identities WHERE issuer=$1 AND account_id=$2`, issuer, account).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		b.AllowEnrollment = true
		return b, nil
	}
	if err != nil {
		return b, err
	}
	b.ExpectedSubject, err = wallet.Decrypt(sealed, p.key)
	return b, err
}
func (p *PurchaseIdentityStore) Consume(ctx context.Context, hash string, b world.Binding, now time.Time) (world.Attempt, error) {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return world.Attempt{}, err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM purchase_orders WHERE id=$1 AND account_id=$2 AND intent_hash=$3 AND state IN ('awaiting_identity','awaiting_approval') FOR UPDATE`, b.OrderID, b.AccountID, b.IntentHash).Scan(&raw)
	if err != nil {
		return world.Attempt{}, world.ErrAttemptUnavailable
	}
	var sealed string
	err = tx.QueryRow(ctx, `SELECT sealed_payload FROM purchase_identity_attempts WHERE state_hash=$1 AND consumed_at IS NULL AND cancelled_at IS NULL AND expires_at>$2 FOR UPDATE`, hash, now).Scan(&sealed)
	if err != nil {
		return world.Attempt{}, world.ErrAttemptUnavailable
	}
	a, err := p.open(sealed)
	if err != nil || a.Binding != b {
		return world.Attempt{}, world.ErrAttemptUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_identity_attempts SET consumed_at=$2 WHERE state_hash=$1`, hash, now); err != nil {
		return world.Attempt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return world.Attempt{}, err
	}
	return a, nil
}
func (p *PurchaseIdentityStore) Complete(ctx context.Context, id string, e world.Evidence, now time.Time) error {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM purchase_orders WHERE id=$1 AND account_id=$2 AND intent_hash=$3 AND state IN ('awaiting_identity','awaiting_approval') FOR UPDATE`, e.Binding.OrderID, e.Binding.AccountID, e.Binding.IntentHash).Scan(&raw)
	o, err := decodeOrder(raw, err)
	if err != nil {
		return world.ErrAttemptUnavailable
	}
	var sealed string
	err = tx.QueryRow(ctx, `SELECT sealed_payload FROM purchase_identity_attempts WHERE id=$1 AND consumed_at IS NOT NULL AND completed_at IS NULL AND cancelled_at IS NULL AND expires_at>$2 FOR UPDATE`, id, now).Scan(&sealed)
	if err != nil {
		return world.ErrAttemptUnavailable
	}
	a, err := p.open(sealed)
	if err != nil || a.ID != e.AttemptID || a.Binding != e.Binding || e.Issuer != a.Issuer || !now.Before(e.ValidUntil) || !now.Before(purchase.Deadline(o)) || !o.MerchantAccepted || e.AuthenticatedAt.Before(o.CreatedAt.Add(-30*time.Second)) || now.Sub(e.AuthenticatedAt) > 5*time.Minute || e.Subject == "" {
		return world.ErrAttemptUnavailable
	}
	if !a.Binding.AllowEnrollment && a.Binding.ExpectedSubject != e.Subject {
		return world.ErrAttemptUnavailable
	}
	subjectHash := purchase.Digest([]byte(e.Issuer + "\x00" + e.Subject))
	sealedSubject, err := wallet.Encrypt(e.Subject, p.key)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO purchase_identities(issuer,subject_hash,sealed_subject,account_id) VALUES($1,$2,$3,$4) ON CONFLICT(issuer,subject_hash) DO NOTHING`, e.Issuer, subjectHash, sealedSubject, e.Binding.AccountID); err != nil {
		return world.ErrAttemptUnavailable
	}
	var owner string
	if err = tx.QueryRow(ctx, `SELECT account_id FROM purchase_identities WHERE issuer=$1 AND subject_hash=$2`, e.Issuer, subjectHash).Scan(&owner); err != nil || owner != e.Binding.AccountID {
		return world.ErrAttemptUnavailable
	}
	o.IdentityVerifiedAt = &e.AuthenticatedAt
	o.IdentityValidUntil = &e.ValidUntil
	o.State = purchase.AwaitingApproval
	raw, err = json.Marshal(o)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_orders SET payload=$1,state=$2,updated_at=now() WHERE id=$3`, string(raw), o.State, o.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_identity_attempts SET completed_at=$2 WHERE id=$1`, id, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (p *PurchaseIdentityStore) Cancel(ctx context.Context, id string, b world.Binding, now time.Time) error {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT payload FROM purchase_orders WHERE id=$1 AND account_id=$2 AND intent_hash=$3 FOR UPDATE`, b.OrderID, b.AccountID, b.IntentHash).Scan(&raw)
	o, err := decodeOrder(raw, err)
	if err != nil {
		return world.ErrAttemptUnavailable
	}
	var sealed string
	err = tx.QueryRow(ctx, `SELECT sealed_payload FROM purchase_identity_attempts WHERE id=$1 FOR UPDATE`, id).Scan(&sealed)
	if err != nil {
		return world.ErrAttemptUnavailable
	}
	a, err := p.open(sealed)
	if err != nil || a.Binding != b {
		return world.ErrAttemptUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE purchase_identity_attempts SET cancelled_at=$2 WHERE id=$1`, id, now); err != nil {
		return err
	}
	if o.SignatureReleasedAt == nil {
		o.IdentityVerifiedAt = nil
		o.IdentityValidUntil = nil
		if o.State == purchase.AwaitingApproval || o.State == purchase.Approved {
			o.State = purchase.AwaitingIdentity
		}
		raw, err = json.Marshal(o)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE purchase_orders SET payload=$1,state=$2 WHERE id=$3`, string(raw), o.State, o.ID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
