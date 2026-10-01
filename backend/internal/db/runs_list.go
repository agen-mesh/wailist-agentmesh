package db

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// Run history: the newest runs of one workflow, or of every workflow a user
// owns, one page at a time.
//
// Pages are cut with a keyset cursor on (started_at, id) rather than OFFSET.
// A run started while someone is paging would shift every OFFSET page by one
// and show a row twice; a cursor always continues from the last row seen.
//
// Each query comes in two forms, with and without a cursor, instead of one
// query with "$n IS NULL OR ...". pgx runs in simple protocol behind
// PgBouncer, where a NULL parameter has no type, and the OR would also stop
// Postgres using the started_at bound to start its index scan.

// RunCursor marks the last row of a page. The next page starts after it.
type RunCursor struct {
	StartedAt time.Time
	ID        string
}

// maxRunCursorLen caps what ParseRunCursor will decode. A real cursor is a
// timestamp and a UUID, well under this.
const maxRunCursorLen = 256

var errInvalidRunCursor = errors.New("invalid cursor")

// Encode returns the cursor as an opaque URL-safe string. The time is written
// in UTC with nanoseconds, which round-trips Postgres' microsecond timestamps
// exactly, so no row is skipped or repeated at a page boundary.
func (c RunCursor) Encode() string {
	raw := c.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// ParseRunCursor reverses Encode. Anything it did not produce is rejected.
func ParseRunCursor(s string) (RunCursor, error) {
	if s == "" || len(s) > maxRunCursorLen {
		return RunCursor{}, errInvalidRunCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return RunCursor{}, errInvalidRunCursor
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return RunCursor{}, errInvalidRunCursor
	}
	startedAt, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return RunCursor{}, errInvalidRunCursor
	}
	return RunCursor{StartedAt: startedAt.UTC(), ID: id}, nil
}

// runSpendJoin adds each page row's spend: everything debit_ledger charged
// this user for the run. It runs over the finished page only, one indexed
// lookup per row (idx_debit_ledger_run), so the sum cannot fan out the page.
const runSpendJoin = `
	LEFT JOIN LATERAL (
		SELECT SUM(d.amount_usd_micros) AS total
		FROM debit_ledger d
		WHERE d.run_id = p.id AND d.user_id = $1
	) s ON true
	ORDER BY p.started_at DESC, p.id DESC`

// The started_at <= bound is redundant with the OR beside it, and there on
// purpose: it is what lets the (workflow_id, started_at DESC) index start at
// the cursor. The OR only breaks ties between runs started in the same
// microsecond.
const workflowRunsQuery = `
	SELECT p.id, p.workflow_id, p.workflow_name, p.triggered_by, p.status,
	       p.started_at, p.finished_at, COALESCE(s.total, 0)
	FROM (
		SELECT r.id, r.workflow_id, w.name AS workflow_name, r.triggered_by,
		       r.status, r.started_at, r.finished_at
		FROM runs r
		JOIN workflows w ON w.id = r.workflow_id
		WHERE w.user_id = $1 AND r.workflow_id = $2
		ORDER BY r.started_at DESC, r.id DESC
		LIMIT $3
	) p` + runSpendJoin

const workflowRunsAfterQuery = `
	SELECT p.id, p.workflow_id, p.workflow_name, p.triggered_by, p.status,
	       p.started_at, p.finished_at, COALESCE(s.total, 0)
	FROM (
		SELECT r.id, r.workflow_id, w.name AS workflow_name, r.triggered_by,
		       r.status, r.started_at, r.finished_at
		FROM runs r
		JOIN workflows w ON w.id = r.workflow_id
		WHERE w.user_id = $1 AND r.workflow_id = $2
		  AND r.started_at <= $4 AND (r.started_at < $4 OR r.id < $5)
		ORDER BY r.started_at DESC, r.id DESC
		LIMIT $3
	) p` + runSpendJoin

// Recent runs across workflows. runs has no user_id, so a plain join would
// read every run of every workflow the user owns before sorting. Instead each
// workflow contributes at most $2 rows straight off its own index, and the
// global sort only ever sees workflows x $2 rows. System workflows (partner
// consoles) are left out, as ListWorkflows leaves them out.
const recentRunsQuery = `
	WITH p AS (
		SELECT r.id, r.workflow_id, w.name AS workflow_name, r.triggered_by,
		       r.status, r.started_at, r.finished_at
		FROM workflows w
		CROSS JOIN LATERAL (
			SELECT r.id, r.workflow_id, r.triggered_by, r.status,
			       r.started_at, r.finished_at
			FROM runs r
			WHERE r.workflow_id = w.id
			ORDER BY r.started_at DESC, r.id DESC
			LIMIT $2
		) r
		WHERE w.user_id = $1 AND NOT w.is_system
		ORDER BY r.started_at DESC, r.id DESC
		LIMIT $2
	)
	SELECT p.id, p.workflow_id, p.workflow_name, p.triggered_by, p.status,
	       p.started_at, p.finished_at, COALESCE(s.total, 0)
	FROM p` + runSpendJoin

const recentRunsAfterQuery = `
	WITH p AS (
		SELECT r.id, r.workflow_id, w.name AS workflow_name, r.triggered_by,
		       r.status, r.started_at, r.finished_at
		FROM workflows w
		CROSS JOIN LATERAL (
			SELECT r.id, r.workflow_id, r.triggered_by, r.status,
			       r.started_at, r.finished_at
			FROM runs r
			WHERE r.workflow_id = w.id
			  AND r.started_at <= $3 AND (r.started_at < $3 OR r.id < $4)
			ORDER BY r.started_at DESC, r.id DESC
			LIMIT $2
		) r
		WHERE w.user_id = $1 AND NOT w.is_system
		ORDER BY r.started_at DESC, r.id DESC
		LIMIT $2
	)
	SELECT p.id, p.workflow_id, p.workflow_name, p.triggered_by, p.status,
	       p.started_at, p.finished_at, COALESCE(s.total, 0)
	FROM p` + runSpendJoin

// ListWorkflowRuns returns up to limit runs of one workflow, newest first,
// starting after cursor when it is not nil. A workflow the user does not own
// yields no rows; the handler checks ownership first so it can answer 404.
func (s *Store) ListWorkflowRuns(ctx context.Context, userID, workflowID string, cursor *RunCursor, limit int) ([]models.RunSummary, error) {
	if cursor == nil {
		return s.queryRunSummaries(ctx, limit, workflowRunsQuery, userID, workflowID, limit)
	}
	return s.queryRunSummaries(ctx, limit, workflowRunsAfterQuery,
		userID, workflowID, limit, cursor.StartedAt.UTC(), cursor.ID)
}

// ListRecentRuns returns up to limit runs across every non-system workflow the
// user owns, newest first, starting after cursor when it is not nil.
func (s *Store) ListRecentRuns(ctx context.Context, userID string, cursor *RunCursor, limit int) ([]models.RunSummary, error) {
	if cursor == nil {
		return s.queryRunSummaries(ctx, limit, recentRunsQuery, userID, limit)
	}
	return s.queryRunSummaries(ctx, limit, recentRunsAfterQuery,
		userID, limit, cursor.StartedAt.UTC(), cursor.ID)
}

func (s *Store) queryRunSummaries(ctx context.Context, limit int, sql string, args ...any) ([]models.RunSummary, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Non-nil even when empty, so the JSON is [] rather than null.
	out := make([]models.RunSummary, 0, limit)
	for rows.Next() {
		var r models.RunSummary
		if err := rows.Scan(
			&r.ID, &r.WorkflowID, &r.WorkflowName, &r.TriggeredBy, &r.Status,
			&r.StartedAt, &r.FinishedAt, &r.SpendUSDMicros,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
