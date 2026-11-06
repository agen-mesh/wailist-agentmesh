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

var (
	ErrShareRate   = errors.New("share rate")
	ErrShareBudget = errors.New("share budget")
)

// WithShareLock holds the share-wallet nonce lock until fn returns.
// The lock is transaction-scoped so a transaction-mode pooler cannot
// hand the same session lock to another client.
func (s *Store) WithShareLock(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7794095)`); err != nil {
		return err
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
