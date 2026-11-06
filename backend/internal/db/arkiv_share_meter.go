package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	MaxSharesPerUserPerHour = 10
	MaxSharesPerDay         = 200
)

// advisoryLockNamespaceArkivShare is the first key of a two-key advisory lock.
// That space does not overlap the single-key locks used for OAuth refresh.
const advisoryLockNamespaceArkivShare int32 = 7794095

var (
	ErrShareRate   = errors.New("share rate")
	ErrShareBudget = errors.New("share budget")
	ErrShareBusy   = errors.New("share busy")
)

// WithShareLock serializes share creates without pinning a pooler connection
// per waiter. A miss returns ErrShareBusy so the caller rolls back at once.
// The lock stays transaction-scoped: a session lock would not survive the
// Supabase transaction pooler.
func (s *Store) WithShareLock(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1, $2)`, advisoryLockNamespaceArkivShare, int32(0)).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return ErrShareBusy
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AllowShare counts rows against now, not the database clock.
// The day cap is checked first so a full day returns ErrShareBudget
// even when the caller is also over the hourly cap.
func AllowShare(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	now = now.UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var dayCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM arkiv_share_meter WHERE created_at >= $1`, dayStart).Scan(&dayCount); err != nil {
		return err
	}
	if dayCount >= MaxSharesPerDay {
		return ErrShareBudget
	}
	var hourCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM arkiv_share_meter
		WHERE user_id = $1 AND created_at > $2`, userID, now.Add(-time.Hour)).Scan(&hourCount); err != nil {
		return err
	}
	if hourCount >= MaxSharesPerUserPerHour {
		return ErrShareRate
	}
	return nil
}

func RecordShare(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO arkiv_share_meter (user_id, created_at) VALUES ($1, $2)`, userID, now.UTC())
	return err
}

func (s *Store) ShareMeterCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM arkiv_share_meter`).Scan(&n)
	return n, err
}
