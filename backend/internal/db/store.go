package db

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrPasswordAccountExists is returned when an OAuth login resolves to an email
// that already belongs to a password account. We refuse to silently link them,
// since our password signup does not verify email ownership — auto-linking would
// allow a pre-registered password account to capture a victim's OAuth identity.
var ErrPasswordAccountExists = errors.New("password account exists for email")

type Store struct {
	pool *pgxpool.Pool
}

func (s *Store) Close() {
	s.pool.Close()
}

// --- Workflow methods ---

func (s *Store) CreateWorkflow(ctx context.Context, name, userID string) (models.Workflow, error) {
	id := uuid.New().String()
	emptyGraph := `{"nodes":[],"edges":[]}`
	var w models.Workflow
	var graphJSON []byte
	var runEndpoint *string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO workflows (id, user_id, name, status, graph)
		VALUES ($1, $2, $3, 'draft', $4::jsonb)
		RETURNING id, user_id, name, status, graph, deployed_at, run_endpoint, created_at, updated_at
	`, id, userID, name, emptyGraph).Scan(
		&w.ID, &w.UserID, &w.Name, &w.Status, &graphJSON,
		&w.DeployedAt, &runEndpoint, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return w, err
	}
	if runEndpoint != nil {
		w.RunEndpoint = *runEndpoint
	}
	unmarshalGraph(graphJSON, &w)
	return w, nil
}

func (s *Store) GetWorkflow(ctx context.Context, id string) (models.Workflow, error) {
	var w models.Workflow
	var graphJSON []byte
	var runEndpoint *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, status, graph, deployed_at, run_endpoint, created_at, updated_at
		FROM workflows WHERE id = $1
	`, id).Scan(
		&w.ID, &w.UserID, &w.Name, &w.Status, &graphJSON,
		&w.DeployedAt, &runEndpoint, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return w, err
	}
	if runEndpoint != nil {
		w.RunEndpoint = *runEndpoint
	}
	unmarshalGraph(graphJSON, &w)
	return w, nil
}

func (s *Store) ListWorkflows(ctx context.Context, userID string) ([]models.Workflow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, status, graph, deployed_at, run_endpoint, created_at, updated_at
		FROM workflows WHERE user_id = $1 ORDER BY updated_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var wfs []models.Workflow
	for rows.Next() {
		var w models.Workflow
		var graphJSON []byte
		var runEndpoint *string
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.Name, &w.Status, &graphJSON,
			&w.DeployedAt, &runEndpoint, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if runEndpoint != nil {
			w.RunEndpoint = *runEndpoint
		}
		unmarshalGraph(graphJSON, &w)
		wfs = append(wfs, w)
	}
	return wfs, rows.Err()
}

func (s *Store) UpdateWorkflow(ctx context.Context, id, name string, graph models.WorkflowGraph) (models.Workflow, error) {
	graphJSON, _ := json.Marshal(graph)
	var w models.Workflow
	var gJSON []byte
	var runEndpoint *string
	err := s.pool.QueryRow(ctx, `
		UPDATE workflows SET name=$2, graph=$3::jsonb, updated_at=NOW()
		WHERE id=$1
		RETURNING id, user_id, name, status, graph, deployed_at, run_endpoint, created_at, updated_at
	`, id, name, string(graphJSON)).Scan(
		&w.ID, &w.UserID, &w.Name, &w.Status, &gJSON,
		&w.DeployedAt, &runEndpoint, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return w, err
	}
	if runEndpoint != nil {
		w.RunEndpoint = *runEndpoint
	}
	unmarshalGraph(gJSON, &w)
	return w, nil
}

func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM workflows WHERE id=$1`, id)
	return err
}

func (s *Store) SetWorkflowDeployed(ctx context.Context, id, runEndpoint string, deployedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE workflows SET status='deployed', run_endpoint=$2, deployed_at=$3, updated_at=NOW()
		WHERE id=$1
	`, id, runEndpoint, deployedAt)
	return err
}

func unmarshalGraph(data []byte, w *models.Workflow) {
	var g models.WorkflowGraph
	if err := json.Unmarshal(data, &g); err == nil {
		w.Nodes = g.Nodes
		w.Edges = g.Edges
	}
}

// --- Run methods ---

func (s *Store) CreateRun(ctx context.Context, workflowID, triggeredBy string, inputContext []byte) (models.Run, error) {
	var r models.Run
	var ic []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO runs (workflow_id, triggered_by, status, input_context)
		VALUES ($1, $2, 'running', $3::jsonb)
		RETURNING id, workflow_id, triggered_by, status, started_at, finished_at, input_context
	`, workflowID, triggeredBy, string(inputContext)).Scan(
		&r.ID, &r.WorkflowID, &r.TriggeredBy, &r.Status,
		&r.StartedAt, &r.FinishedAt, &ic,
	)
	if err != nil {
		return r, err
	}
	if ic != nil {
		json.Unmarshal(ic, &r.InputContext)
	}
	return r, nil
}

func (s *Store) GetRun(ctx context.Context, runID string) (models.Run, error) {
	var r models.Run
	var ic []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, workflow_id, triggered_by, status, started_at, finished_at, input_context
		FROM runs WHERE id=$1
	`, runID).Scan(
		&r.ID, &r.WorkflowID, &r.TriggeredBy, &r.Status,
		&r.StartedAt, &r.FinishedAt, &ic,
	)
	if err != nil {
		return r, err
	}
	if ic != nil {
		json.Unmarshal(ic, &r.InputContext)
	}
	return r, nil
}

func (s *Store) FinishRun(ctx context.Context, runID string, status models.RunStatus) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE runs SET status=$2, finished_at=NOW() WHERE id=$1
	`, runID, string(status))
	return err
}

// --- RunLog methods ---

func (s *Store) InsertRunLog(ctx context.Context, l models.RunLog) (models.RunLog, error) {
	inputJSON, _ := json.Marshal(l.Input)
	var out models.RunLog
	var inJSON, outJSON []byte
	var durationMs *int
	err := s.pool.QueryRow(ctx, `
		INSERT INTO run_logs (run_id, step_index, node_id, node_type, status, input)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb)
		RETURNING id, run_id, step_index, node_id, node_type, status, input, output, duration_ms, ts
	`, l.RunID, l.StepIndex, l.NodeID, string(l.NodeType), string(l.Status), string(inputJSON)).Scan(
		&out.ID, &out.RunID, &out.StepIndex, &out.NodeID, &out.NodeType,
		&out.Status, &inJSON, &outJSON, &durationMs, &out.Ts,
	)
	if err != nil {
		return out, err
	}
	if durationMs != nil {
		out.DurationMs = *durationMs
	}
	if inJSON != nil {
		json.Unmarshal(inJSON, &out.Input)
	}
	return out, nil
}

func (s *Store) UpdateRunLog(ctx context.Context, id string, status models.LogStatus, outputJSON []byte, durationMs int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE run_logs SET status=$2, output=$3::jsonb, duration_ms=$4 WHERE id=$1
	`, id, string(status), string(outputJSON), durationMs)
	return err
}

func (s *Store) GetRunLogs(ctx context.Context, runID string) ([]models.RunLog, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, step_index, node_id, node_type, status, output, duration_ms, ts
		FROM run_logs WHERE run_id=$1 ORDER BY step_index, ts
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []models.RunLog
	for rows.Next() {
		var l models.RunLog
		var outJSON []byte
		var durationMs *int
		if err := rows.Scan(
			&l.ID, &l.RunID, &l.StepIndex, &l.NodeID, &l.NodeType,
			&l.Status, &outJSON, &durationMs, &l.Ts,
		); err != nil {
			return nil, err
		}
		if durationMs != nil {
			l.DurationMs = *durationMs
		}
		if outJSON != nil {
			json.Unmarshal(outJSON, &l.Output)
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// --- AgentWallet methods ---

func (s *Store) InsertAgentWallet(ctx context.Context, w models.AgentWallet) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_wallets (workflow_id, agent_node_id, address, encrypted_mnemonic, network)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (workflow_id, agent_node_id) DO UPDATE
		  SET address=EXCLUDED.address, encrypted_mnemonic=EXCLUDED.encrypted_mnemonic
	`, w.WorkflowID, w.AgentNodeID, w.Address, w.EncryptedMnemonic, w.Network)
	return err
}

func (s *Store) GetAgentWallet(ctx context.Context, workflowID, agentNodeID string) (models.AgentWallet, error) {
	var w models.AgentWallet
	err := s.pool.QueryRow(ctx, `
		SELECT id, workflow_id, agent_node_id, address, encrypted_mnemonic, network
		FROM agent_wallets WHERE workflow_id=$1 AND agent_node_id=$2
	`, workflowID, agentNodeID).Scan(
		&w.ID, &w.WorkflowID, &w.AgentNodeID, &w.Address, &w.EncryptedMnemonic, &w.Network,
	)
	return w, err
}

func (s *Store) ListAgentWallets(ctx context.Context, workflowID string) ([]models.AgentWallet, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_id, agent_node_id, address, encrypted_mnemonic, network
		FROM agent_wallets WHERE workflow_id=$1
	`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var wallets []models.AgentWallet
	for rows.Next() {
		var w models.AgentWallet
		if err := rows.Scan(&w.ID, &w.WorkflowID, &w.AgentNodeID, &w.Address, &w.EncryptedMnemonic, &w.Network); err != nil {
			return nil, err
		}
		wallets = append(wallets, w)
	}
	return wallets, rows.Err()
}

// --- User methods ---

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (id, email, password_hash)
		VALUES (gen_random_uuid()::text, $1, $2)
		RETURNING id, email, password_hash, created_at
	`, email, passwordHash).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, created_at
		FROM users WHERE email = $1
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

// GetOrCreateOAuthUser returns the user for a verified OAuth email, creating an
// OAuth-only account (empty password_hash, so bcrypt password login always fails)
// when none exists. Linking to an existing OAuth account by verified email is
// allowed; linking to a password account returns ErrPasswordAccountExists.
func (s *Store) GetOrCreateOAuthUser(ctx context.Context, email string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, created_at FROM users WHERE email = $1
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if err == nil {
		if u.PasswordHash != "" {
			return models.User{}, ErrPasswordAccountExists
		}
		return u, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, err
	}

	// No existing user — create an OAuth-only account.
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (id, email, password_hash)
		VALUES (gen_random_uuid()::text, $1, '')
		ON CONFLICT (email) DO NOTHING
		RETURNING id, email, password_hash, created_at
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Lost a race: a row appeared between SELECT and INSERT. Re-fetch and
		// apply the same password-account guard.
		err = s.pool.QueryRow(ctx, `
			SELECT id, email, password_hash, created_at FROM users WHERE email = $1
		`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
		if err == nil && u.PasswordHash != "" {
			return models.User{}, ErrPasswordAccountExists
		}
	}
	return u, err
}

// --- Waitlist methods ---

func (s *Store) InsertWaitlistEmail(ctx context.Context, email string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO waitlist (email) VALUES ($1) ON CONFLICT (email) DO NOTHING
	`, email)
	return err
}

// --- Credit ledger methods ---

func (s *Store) CreateCreditTransaction(ctx context.Context, userID, providerOrderID string, amountINRPaise int64, fxRate float64) (models.CreditTransaction, error) {
	creditUSDMicros := int64(math.Round(float64(amountINRPaise) / 100.0 * fxRate * 1e6))
	var txn models.CreditTransaction
	err := s.pool.QueryRow(ctx, `
		INSERT INTO credit_ledger (user_id, provider, provider_order_id, status, amount_inr_paise, fx_rate_usd_per_inr, credit_usd_micros)
		VALUES ($1, 'razorpay', $2, 'pending', $3, $4, $5)
		RETURNING id, user_id, provider, provider_order_id, status, amount_inr_paise, fx_rate_usd_per_inr, credit_usd_micros, created_at
	`, userID, providerOrderID, amountINRPaise, fxRate, creditUSDMicros).Scan(
		&txn.ID, &txn.UserID, &txn.Provider, &txn.ProviderOrderID, &txn.Status,
		&txn.AmountINRPaise, &txn.FXRateUSDPerINR, &txn.CreditUSDMicros, &txn.CreatedAt,
	)
	return txn, err
}

// CreateCryptoCreditTransaction records a pending ledger row for a hosted crypto invoice
// (NOWPayments or any future crypto gateway sharing this shape). Unlike the Razorpay path,
// the amount is already USD-denominated by the gateway, so there is no FX rate to store.
func (s *Store) CreateCryptoCreditTransaction(ctx context.Context, userID, provider, providerOrderID string, amountUSDCents int64) (models.CreditTransaction, error) {
	creditUSDMicros := amountUSDCents * 10_000
	var txn models.CreditTransaction
	err := s.pool.QueryRow(ctx, `
		INSERT INTO credit_ledger (user_id, provider, provider_order_id, status, amount_usd_cents, credit_usd_micros)
		VALUES ($1, $2, $3, 'pending', $4, $5)
		RETURNING id, user_id, provider, provider_order_id, status, amount_usd_cents, credit_usd_micros, created_at
	`, userID, provider, providerOrderID, amountUSDCents, creditUSDMicros).Scan(
		&txn.ID, &txn.UserID, &txn.Provider, &txn.ProviderOrderID, &txn.Status,
		&txn.AmountUSDCents, &txn.CreditUSDMicros, &txn.CreatedAt,
	)
	return txn, err
}

// ErrCreditTransactionNotFound is returned when no credit_ledger row exists for the given
// provider order ID — the caller supplied an order Razorpay never told us about (or that
// our own CreateCreditTransaction failed to record). Callers should treat this as a
// permanent 4xx, not a transient failure: retrying an unknown order will never succeed.
var ErrCreditTransactionNotFound = errors.New("credit transaction not found")

// CompleteCreditTransaction marks the ledger row for providerOrderID as completed and
// credits the user's cached balance, atomically. Idempotent: if the row is already
// completed (webhook/verify replay), it returns the stored amount without re-crediting.
// The bool return is true only when this call is the one that actually completed the
// transaction (false on a replay) — callers use it to fire an audit-log notification
// exactly once per real credit, not once per redundant client-verify/webhook race.
func (s *Store) CompleteCreditTransaction(ctx context.Context, provider, providerOrderID, providerPaymentID string) (int64, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	var (
		id              string
		userID          string
		status          string
		creditUSDMicros int64
		completedAt     *time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, status, credit_usd_micros, completed_at
		FROM credit_ledger
		WHERE provider_order_id = $1 AND provider = $2
		FOR UPDATE
	`, providerOrderID, provider).Scan(&id, &userID, &status, &creditUSDMicros, &completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, ErrCreditTransactionNotFound
	}
	if err != nil {
		return 0, false, err
	}

	// Gate on completed_at (replay-safety, unchanged) *and* on status != 'failed': a row
	// a crypto webhook already marked failed/expired must never be resurrected by a
	// late or out-of-order "finished" IPN retry — see MarkCreditTransactionStatus.
	if completedAt != nil || status == "failed" {
		return creditUSDMicros, false, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE credit_ledger SET status = 'completed', provider_payment_id = $1, completed_at = NOW()
		WHERE id = $2
	`, providerPaymentID, id); err != nil {
		return 0, false, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE users SET credit_balance_usd_micros = credit_balance_usd_micros + $1 WHERE id = $2
	`, creditUSDMicros, userID); err != nil {
		return 0, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return creditUSDMicros, true, nil
}

// RefundCreditTransaction reverses previously-credited USD micros when Razorpay reports a
// refund against an order. totalRefundedINRPaise is the *cumulative* amount refunded on the
// payment so far — Razorpay resends this on every refund event (partial or full), so this
// method tracks refunded_inr_paise on the ledger row and only acts on the delta between the
// new total and what was already applied, making repeated/replayed events safe.
//
// If the order was never completed in our ledger (still 'pending' or already 'expired'), no
// credit was ever granted, so no balance reversal happens — only the bookkeeping columns are
// updated. credit_balance_usd_micros is floored at 0 via GREATEST so a reversal can never push
// a user negative even under an unexpected ordering of events.
//
// The bool return is true only when this call applied a new refund delta (false when the
// cumulative total matches what's already recorded, i.e. a replayed webhook) — callers use
// it to fire an audit-log notification exactly once per real refund event.
func (s *Store) RefundCreditTransaction(ctx context.Context, providerOrderID string, totalRefundedINRPaise int64) (int64, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	var (
		id               string
		userID           string
		status           string
		amountINRPaise   int64
		fxRate           float64
		refundedINRPaise int64
	)
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, status, amount_inr_paise, fx_rate_usd_per_inr, refunded_inr_paise
		FROM credit_ledger
		WHERE provider_order_id = $1 AND provider = 'razorpay'
		FOR UPDATE
	`, providerOrderID).Scan(&id, &userID, &status, &amountINRPaise, &fxRate, &refundedINRPaise)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, ErrCreditTransactionNotFound
	}
	if err != nil {
		return 0, false, err
	}

	delta := totalRefundedINRPaise - refundedINRPaise
	if delta <= 0 {
		return 0, false, nil
	}

	var reversedUSDMicros int64
	if status == "completed" || status == "refunded" {
		reversedUSDMicros = int64(math.Round(float64(delta) / 100.0 * fxRate * 1e6))
		if _, err := tx.Exec(ctx, `
			UPDATE users SET credit_balance_usd_micros = GREATEST(0, credit_balance_usd_micros - $1) WHERE id = $2
		`, reversedUSDMicros, userID); err != nil {
			return 0, false, err
		}
	}

	newStatus := status
	if totalRefundedINRPaise >= amountINRPaise {
		newStatus = "refunded"
	}

	if _, err := tx.Exec(ctx, `
		UPDATE credit_ledger SET refunded_inr_paise = $1, status = $2 WHERE id = $3
	`, totalRefundedINRPaise, newStatus, id); err != nil {
		return 0, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return reversedUSDMicros, true, nil
}

func (s *Store) GetCreditBalance(ctx context.Context, userID string) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx, `SELECT credit_balance_usd_micros FROM users WHERE id = $1`, userID).Scan(&balance)
	return balance, err
}

// MarkCreditTransactionStatus moves a still-pending ledger row directly to status
// (e.g. "failed"/"expired" for a NOWPayments IPN that will never complete, or "partial"
// for partially_paid) without touching the user's balance — a pending row never credited
// anything, so there's nothing to reverse. No-op if the row is no longer pending, so it's
// safe to call on IPN replays.
func (s *Store) MarkCreditTransactionStatus(ctx context.Context, provider, providerOrderID, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE credit_ledger SET status = $1
		WHERE provider_order_id = $2 AND provider = $3 AND status = 'pending'
	`, status, providerOrderID, provider)
	return err
}

// ExpireStalePendingTransactions marks credit_ledger rows for provider still 'pending'
// after olderThan as 'expired' — checkouts the user opened but never completed (closed
// tab, abandoned QR scan, on-chain payment never sent). Scoped to a single provider so
// callers can use a per-provider staleness window: fast checkout providers like Razorpay
// warrant a short window, while on-chain crypto providers like NOWPayments need a much
// longer one to avoid expiring payments still working through block confirmations. Keeps
// 'pending' meaningful as "still in progress" rather than accumulating dead rows.
func (s *Store) ExpireStalePendingTransactions(ctx context.Context, provider string, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(ctx, `
		UPDATE credit_ledger SET status = 'expired'
		WHERE status = 'pending' AND provider = $1 AND created_at < $2
	`, provider, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- Debit ledger methods ---

// ErrInsufficientCredits is returned by DebitCredits when the user's balance
// is below the amount being charged. Callers treat this as a permanent
// failure for that call — the node did not run (or, for x402, the payment
// already happened and this is logged rather than retried).
var ErrInsufficientCredits = errors.New("insufficient credits")

// DebitCredits atomically charges a user's credit balance for a metered
// action inside a workflow run, and records the charge in debit_ledger.
// Locks the user row for the duration of the check-and-decrement — same
// pattern as CompleteCreditTransaction — so concurrent debits against the
// same user can never push the balance negative.
func (s *Store) DebitCredits(ctx context.Context, userID string, amountUSDMicros int64, kind, workflowID, runID, nodeID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var balance int64
	if err := tx.QueryRow(ctx, `
		SELECT credit_balance_usd_micros FROM users WHERE id = $1 FOR UPDATE
	`, userID).Scan(&balance); err != nil {
		return err
	}

	if balance < amountUSDMicros {
		return ErrInsufficientCredits
	}

	if _, err := tx.Exec(ctx, `
		UPDATE users SET credit_balance_usd_micros = credit_balance_usd_micros - $1 WHERE id = $2
	`, amountUSDMicros, userID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO debit_ledger (user_id, workflow_id, run_id, node_id, kind, amount_usd_micros)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, workflowID, runID, nodeID, kind, amountUSDMicros); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ListDebitLedger returns every debit_ledger row for a run, oldest first.
// Used by the credits/usage dashboard and by tests asserting exactly which
// charges a run produced.
func (s *Store) ListDebitLedger(ctx context.Context, runID string) ([]models.DebitEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, workflow_id, run_id, node_id, kind, amount_usd_micros, created_at
		FROM debit_ledger WHERE run_id = $1 ORDER BY created_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.DebitEntry
	for rows.Next() {
		var e models.DebitEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.WorkflowID, &e.RunID, &e.NodeID, &e.Kind, &e.AmountUSDMicros, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- X402 Relay Settlement methods ---

// ErrDuplicateSettlement is returned when an inbound settlement's txid has already
// been recorded — a replayed X-PAYMENT payload must never be processed twice.
var ErrDuplicateSettlement = errors.New("duplicate settlement txid")

func (s *Store) RecordInboundSettlement(ctx context.Context, targetURL, inboundTxID string, amountAssetMicros int64) (models.X402RelaySettlement, error) {
	var row models.X402RelaySettlement
	err := s.pool.QueryRow(ctx, `
		INSERT INTO x402_relay_settlements (target_url, inbound_tx_id, amount_asset_micros)
		VALUES ($1, $2, $3)
		RETURNING id, target_url, inbound_tx_id, outbound_tx_id, amount_asset_micros, status, created_at
	`, targetURL, inboundTxID, amountAssetMicros).Scan(
		&row.ID, &row.TargetURL, &row.InboundTxID, &row.OutboundTxID, &row.AmountAssetMicros, &row.Status, &row.CreatedAt,
	)
	if err != nil && strings.Contains(err.Error(), "duplicate key value") {
		return models.X402RelaySettlement{}, ErrDuplicateSettlement
	}
	return row, err
}

func (s *Store) RecordOutboundSettlement(ctx context.Context, id, outboundTxID, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE x402_relay_settlements SET outbound_tx_id = $2, status = $3 WHERE id = $1
	`, id, outboundTxID, status)
	return err
}

// GetX402RelaySettlementByInboundTx looks up a relay ledger row by its
// inbound settlement tx id — used to verify what was actually recorded
// (e.g. the settled amount) after a relay flow completes.
func (s *Store) GetX402RelaySettlementByInboundTx(ctx context.Context, inboundTxID string) (models.X402RelaySettlement, error) {
	var row models.X402RelaySettlement
	err := s.pool.QueryRow(ctx, `
		SELECT id, target_url, inbound_tx_id, outbound_tx_id, amount_asset_micros, status, created_at
		FROM x402_relay_settlements WHERE inbound_tx_id = $1
	`, inboundTxID).Scan(
		&row.ID, &row.TargetURL, &row.InboundTxID, &row.OutboundTxID, &row.AmountAssetMicros, &row.Status, &row.CreatedAt,
	)
	return row, err
}
