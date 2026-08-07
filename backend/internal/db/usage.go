package db

import (
	"context"
	"time"
)

// Usage reporting reads debit_ledger — the same rows the engine writes when it
// actually charges a user (byok_flat_fee for an LLM step, x402_platform_fee
// and x402_relay_cost for a paid x402 call). That makes these numbers the real
// spend, not an estimate reconstructed from logs.
//
// A single paid x402 call can produce two ledger rows (the platform fee and
// the relay cost), so anything counting *calls* counts distinct (run, node)
// pairs rather than rows — otherwise every call would report as two.

const x402KindFilter = `kind <> 'byok_flat_fee'`

type UsageTotals struct {
	TotalUSDMicros int64
	X402Calls      int64
}

// UsageTotalsSince returns spend and call count for one window. Callers ask
// twice (current window, preceding window of equal length) to compute a delta.
func (s *Store) UsageTotalsSince(ctx context.Context, userID string, since time.Time, until *time.Time) (UsageTotals, error) {
	var t UsageTotals
	query := `
		SELECT COALESCE(SUM(amount_usd_micros), 0),
		       COUNT(DISTINCT (run_id, node_id)) FILTER (WHERE ` + x402KindFilter + `)
		FROM debit_ledger
		WHERE user_id = $1 AND created_at >= $2 AND ($3::timestamptz IS NULL OR created_at < $3)`
	err := s.pool.QueryRow(ctx, query, userID, since, until).Scan(&t.TotalUSDMicros, &t.X402Calls)
	return t, err
}

type UsageBucket struct {
	Bucket        time.Time
	X402USDMicros int64
	LLMUSDMicros  int64
	Calls         int64
}

// UsageTimeseries buckets spend by hour or day. bucket is validated by the
// caller against a fixed set — it reaches date_trunc as a bind parameter, so
// it can never be SQL, but an unrecognised value would still error at runtime.
func (s *Store) UsageTimeseries(ctx context.Context, userID string, since time.Time, bucket string) ([]UsageBucket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT date_trunc($3, created_at) AS b,
		       COALESCE(SUM(amount_usd_micros) FILTER (WHERE `+x402KindFilter+`), 0),
		       COALESCE(SUM(amount_usd_micros) FILTER (WHERE kind = 'byok_flat_fee'), 0),
		       COUNT(DISTINCT (run_id, node_id)) FILTER (WHERE `+x402KindFilter+`)
		FROM debit_ledger
		WHERE user_id = $1 AND created_at >= $2
		GROUP BY b ORDER BY b`, userID, since, bucket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UsageBucket
	for rows.Next() {
		var b UsageBucket
		if err := rows.Scan(&b.Bucket, &b.X402USDMicros, &b.LLMUSDMicros, &b.Calls); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type WorkflowSpendRow struct {
	WorkflowID string
	Name       string
	Status     string
	USDMicros  int64
	Calls      int64
}

func (s *Store) UsageByWorkflow(ctx context.Context, userID string, since time.Time) ([]WorkflowSpendRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.workflow_id, w.name, w.status,
		       COALESCE(SUM(d.amount_usd_micros), 0),
		       COUNT(DISTINCT (d.run_id, d.node_id)) FILTER (WHERE d.`+x402KindFilter+`)
		FROM debit_ledger d
		JOIN workflows w ON w.id = d.workflow_id
		WHERE d.user_id = $1 AND d.created_at >= $2
		GROUP BY d.workflow_id, w.name, w.status
		ORDER BY 4 DESC`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WorkflowSpendRow
	for rows.Next() {
		var r WorkflowSpendRow
		if err := rows.Scan(&r.WorkflowID, &r.Name, &r.Status, &r.USDMicros, &r.Calls); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type EndpointSpendRow struct {
	Endpoint   string
	Name       string
	IsX402     bool
	USDMicros  int64
	Calls      int64
	LastUsedAt time.Time
}

// UsageByEndpoint resolves each ledger row's node_id back to the node inside
// its workflow's graph JSON, so spend can be attributed to the actual URL that
// was called rather than an opaque node id. A node deleted since the call
// leaves no match; those rows fall back to the node id so their spend is still
// reported instead of silently vanishing from the totals.
func (s *Store) UsageByEndpoint(ctx context.Context, userID string, since time.Time) ([]EndpointSpendRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(n.node->>'endpoint', ''), NULLIF(n.node->>'url', ''), d.node_id) AS endpoint,
		       COALESCE(n.node->>'name', '') AS name,
		       bool_or(d.`+x402KindFilter+`) AS is_x402,
		       COALESCE(SUM(d.amount_usd_micros), 0),
		       COUNT(DISTINCT (d.run_id, d.node_id)),
		       MAX(d.created_at)
		FROM debit_ledger d
		JOIN workflows w ON w.id = d.workflow_id
		LEFT JOIN LATERAL (
			SELECT elem AS node
			FROM jsonb_array_elements(w.graph->'nodes') elem
			WHERE elem->>'id' = d.node_id
			LIMIT 1
		) n ON true
		WHERE d.user_id = $1 AND d.created_at >= $2
		GROUP BY 1, 2
		ORDER BY 4 DESC`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EndpointSpendRow
	for rows.Next() {
		var r EndpointSpendRow
		if err := rows.Scan(&r.Endpoint, &r.Name, &r.IsX402, &r.USDMicros, &r.Calls, &r.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
