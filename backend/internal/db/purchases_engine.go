package db

import (
	"context"
	"errors"

	"github.com/agentmesh/backend/internal/purchase"
	"github.com/jackc/pgx/v5"
)

var ErrPurchaseWaiting = errors.New("workflow has a purchase waiting for resolution; resume or stop that run")

func lockPurchaseAdmission(ctx context.Context, tx pgx.Tx, workflowID string) error {
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM workflows WHERE id=$1 FOR UPDATE`, workflowID).Scan(&id); err != nil {
		return err
	}
	var waiting bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE workflow_id=$1 AND status='waiting')`, workflowID).Scan(&waiting); err != nil {
		return err
	}
	if waiting {
		return ErrPurchaseWaiting
	}
	return nil
}
func (s *Store) WaitForPurchase(ctx context.Context, runID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var workflow string
	if err = tx.QueryRow(ctx, `SELECT workflow_id FROM runs WHERE id=$1`, runID).Scan(&workflow); err != nil {
		return err
	}
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM workflows WHERE id=$1 FOR UPDATE`, workflow).Scan(&id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE runs SET status='waiting',finished_at=NULL WHERE id=$1 AND status='running'`, runID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return purchase.ErrConflict
	}
	return tx.Commit(ctx)
}
func (s *Store) StopWaitingPurchaseRuns(ctx context.Context, workflowID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE runs SET status='stopped',finished_at=now() WHERE workflow_id=$1 AND status='waiting'`, workflowID)
	return tag.RowsAffected() > 0, err
}

func (p *PurchaseStore) Pending(ctx context.Context) ([]purchase.Order, error) {
	rows, err := p.store.pool.Query(ctx, `SELECT payload FROM purchase_orders WHERE state IN ('authorization_released','submitting','payment_unknown','paid','fulfilling','fulfillment_unknown') OR (coalesce(payload->>'signedEnvelope','') <> '' AND coalesce(payload->>'relayConfirmed','false') <> 'true') ORDER BY updated_at LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []purchase.Order
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		o, err := decodeOrder(raw, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
