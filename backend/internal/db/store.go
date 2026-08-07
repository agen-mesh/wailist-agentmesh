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
	// coupons is the redeemable coupon catalog (code -> USD micros), loaded
	// from configuration at startup. See SetCouponCatalog.
	coupons map[string]int64
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

// FindSystemWorkflow is GetOrCreateSystemWorkflow's read-only half: looks
// up this user's workflow row with the given name WITHOUT creating one if
// it's missing. found is false, with a zero Workflow, when there's nothing
// to find yet -- that is a normal, expected outcome for a user who has
// never triggered the system workflow's creation, not an error condition.
//
// Exists specifically for callers that need to know "is THIS id the system
// workflow" (e.g. WorkflowRoute deciding whether to render the Tendril
// console instead of the canvas for a given workflowId) without the side
// effect of silently creating a hidden row the moment they check -- every
// workflow-page visit calling GetOrCreateSystemWorkflow instead would mint
// an empty "Tendril Console" row for every user who has never touched
// Tendril, the instant they open ANY of their own workflows.
func (s *Store) FindSystemWorkflow(ctx context.Context, userID, name string) (w models.Workflow, found bool, err error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, status, graph, deployed_at, run_endpoint, created_at, updated_at
		FROM workflows WHERE user_id = $1 AND name = $2 ORDER BY created_at ASC LIMIT 1
	`, userID, name)
	if err != nil {
		return models.Workflow{}, false, err
	}
	for rows.Next() {
		var graphJSON []byte
		var runEndpoint *string
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.Name, &w.Status, &graphJSON,
			&w.DeployedAt, &runEndpoint, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			rows.Close()
			return models.Workflow{}, false, err
		}
		if runEndpoint != nil {
			w.RunEndpoint = *runEndpoint
		}
		unmarshalGraph(graphJSON, &w)
		found = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return models.Workflow{}, false, err
	}
	return w, found, nil
}

// GetOrCreateSystemWorkflow returns this user's workflow row with the given
// name, creating it if it doesn't exist yet. Backs direct-action UIs (the
// Tendril console) that never show a canvas but still need a real
// workflow_id/run_id pair for the engine's node executors and the FK
// constraints on rows like tendril_leases. Use FindSystemWorkflow instead
// when a missing row should mean "not found", not "create one now".
func (s *Store) GetOrCreateSystemWorkflow(ctx context.Context, userID, name string) (models.Workflow, error) {
	w, found, err := s.FindSystemWorkflow(ctx, userID, name)
	if err != nil {
		return models.Workflow{}, err
	}
	if found {
		return w, nil
	}
	return s.CreateWorkflow(ctx, name, userID)
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
		RETURNING id, email, password_hash, name, org_name, created_at
	`, email, passwordHash).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.OrgName, &u.CreatedAt)
	return u, err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, name, org_name, created_at
		FROM users WHERE email = $1
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.OrgName, &u.CreatedAt)
	return u, err
}

func (s *Store) GetUserByID(ctx context.Context, id string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, name, org_name, created_at
		FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.OrgName, &u.CreatedAt)
	return u, err
}

// UpdateProfile sets the display name and organization name for a user —
// collected at signup for password accounts, or via a post-login onboarding
// step for OAuth accounts (which have no name/org until the provider redirect
// completes, since neither Google nor GitHub's basic profile scope carries one).
func (s *Store) UpdateProfile(ctx context.Context, userID, name, orgName string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		UPDATE users SET name = $2, org_name = $3
		WHERE id = $1
		RETURNING id, email, password_hash, name, org_name, created_at
	`, userID, name, orgName).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.OrgName, &u.CreatedAt)
	return u, err
}

// GetOrCreateOAuthUser returns the user for a verified OAuth email, creating an
// OAuth-only account (empty password_hash, so bcrypt password login always fails)
// when none exists. Linking to an existing OAuth account by verified email is
// allowed; linking to a password account returns ErrPasswordAccountExists.
func (s *Store) GetOrCreateOAuthUser(ctx context.Context, email string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, name, org_name, created_at FROM users WHERE email = $1
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.OrgName, &u.CreatedAt)
	if err == nil {
		if u.PasswordHash != "" {
			return models.User{}, ErrPasswordAccountExists
		}
		return u, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, err
	}

	// No existing user — create an OAuth-only account. name/org_name are left
	// blank: neither provider's basic profile scope carries an organization,
	// and the frontend prompts for both once the user lands back on the app
	// (see Deps.Me's needsOnboarding and Deps.UpdateProfile).
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (id, email, password_hash)
		VALUES (gen_random_uuid()::text, $1, '')
		ON CONFLICT (email) DO NOTHING
		RETURNING id, email, password_hash, name, org_name, created_at
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.OrgName, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Lost a race: a row appeared between SELECT and INSERT. Re-fetch and
		// apply the same password-account guard.
		err = s.pool.QueryRow(ctx, `
			SELECT id, email, password_hash, name, org_name, created_at FROM users WHERE email = $1
		`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.OrgName, &u.CreatedAt)
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
	return s.CreateCreditTransactionForProvider(ctx, "cashfree", userID, providerOrderID, amountINRPaise, fxRate)
}

func (s *Store) CreateCreditTransactionForProvider(ctx context.Context, provider, userID, providerOrderID string, amountINRPaise int64, fxRate float64) (models.CreditTransaction, error) {
	creditUSDMicros := int64(math.Round(float64(amountINRPaise) / 100.0 * fxRate * 1e6))
	var txn models.CreditTransaction
	err := s.pool.QueryRow(ctx, `
		INSERT INTO credit_ledger (user_id, provider, provider_order_id, status, amount_inr_paise, fx_rate_usd_per_inr, credit_usd_micros)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6)
		RETURNING id, user_id, provider, provider_order_id, status, amount_inr_paise, fx_rate_usd_per_inr, credit_usd_micros, created_at
	`, userID, provider, providerOrderID, amountINRPaise, fxRate, creditUSDMicros).Scan(
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
		WHERE provider_order_id = $1
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

// CreditBalance is a thin alias for GetCreditBalance, named to match
// nodes.TendrilStore's method set (TendrilCreditBalance/CreditBalance read
// as a pair there) without a second implementation of the same query.
func (s *Store) CreditBalance(ctx context.Context, userID string) (int64, error) {
	return s.GetCreditBalance(ctx, userID)
}

// --- Coupons ---

// The coupon catalog is configuration, not code: it comes from the COUPON_CODES
// env var (parsed by ParseCouponCatalog, installed via SetCouponCatalog at
// startup) so codes can be minted, repriced, or retired without a deploy. An
// empty catalog — the zero value, and what an unset COUPON_CODES gives you —
// means every code is rejected as invalid, which is the right default: a
// forgotten hardcoded code that still grants real credits is a standing
// liability.
//
// Each code is independently redeemable once per user (enforced by the UNIQUE
// (user_id, code) constraint on coupon_redemptions) — redeeming multiple
// distinct codes stacks.

var (
	ErrCouponInvalid         = errors.New("invalid coupon code")
	ErrCouponAlreadyRedeemed = errors.New("coupon already redeemed")
)

// SetCouponCatalog installs the redeemable coupon catalog, keyed by
// already-uppercased code, valued in USD micros. Called once at startup,
// before the server accepts requests.
func (s *Store) SetCouponCatalog(catalog map[string]int64) {
	s.coupons = catalog
}

// CouponCatalog returns the installed catalog (nil if none was set).
func (s *Store) CouponCatalog() map[string]int64 {
	return s.coupons
}

// ParseCouponCatalog parses a COUPON_CODES spec into a catalog. The format is
// a comma-separated list of CODE:AMOUNT pairs, where AMOUNT is in US dollars:
//
//	COUPON_CODES="WELCOME5:5,LAUNCH:12.50"
//
// Codes are upper-cased to match the handler, which upper-cases user input
// before lookup. An empty spec yields an empty catalog with no error; a
// malformed entry is an error rather than a silent skip, so a typo in one code
// can't quietly leave a coupon campaign half-live.
//
// A repeated code is an error for the same reason. Last-write-wins on a
// duplicate would mean "WELCOME5:5,WELCOME5:10" quietly grants $10 while
// reading, to anyone scanning the config, like it grants $5 — the exact class
// of silent misconfiguration the rest of this parser refuses.
func ParseCouponCatalog(spec string) (map[string]int64, error) {
	catalog := map[string]int64{}
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		code, amountStr, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf("coupon entry %q is not in CODE:AMOUNT form", entry)
		}
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			return nil, fmt.Errorf("coupon entry %q has an empty code", entry)
		}
		if _, dup := catalog[code]; dup {
			return nil, fmt.Errorf("coupon %s: listed more than once", code)
		}
		usd, err := strconv.ParseFloat(strings.TrimSpace(amountStr), 64)
		if err != nil {
			return nil, fmt.Errorf("coupon %s: amount %q is not a number", code, strings.TrimSpace(amountStr))
		}
		if usd <= 0 {
			return nil, fmt.Errorf("coupon %s: amount must be greater than 0", code)
		}
		// Round rather than truncate: 0.1 lands a hair under 100000 micros in
		// binary floating point, and truncating would quietly short every
		// redemption by one micro.
		catalog[code] = int64(math.Round(usd * 1e6))
	}
	return catalog, nil
}

// RedeemCoupon credits a user's balance for an unredeemed, known coupon code,
// atomically. The UNIQUE (user_id, code) constraint plus ON CONFLICT DO
// NOTHING is what actually enforces "once per user per code" under
// concurrent requests — RowsAffected == 0 means another request (or an
// earlier one) already claimed this code for this user.
//
// Returns the user's new balance and the amount this redemption credited, both
// in USD micros — the caller shows the credited amount, which is per-code
// configuration and no longer a fixed $5 it could hardcode.
func (s *Store) RedeemCoupon(ctx context.Context, userID, code string) (newBalance, credited int64, err error) {
	amount, ok := s.coupons[code]
	if !ok {
		return 0, 0, ErrCouponInvalid
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		INSERT INTO coupon_redemptions (user_id, code, credit_usd_micros)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, code) DO NOTHING
	`, userID, code, amount)
	if err != nil {
		return 0, 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, 0, ErrCouponAlreadyRedeemed
	}

	if err := tx.QueryRow(ctx, `
		UPDATE users SET credit_balance_usd_micros = credit_balance_usd_micros + $1
		WHERE id = $2
		RETURNING credit_balance_usd_micros
	`, amount, userID).Scan(&newBalance); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return newBalance, amount, nil
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

// debitCredits atomically locks userID's balance, checks it covers
// amountUSDMicros, decrements it, then lets insertLedger write whatever
// debit_ledger row shape the caller needs inside the same transaction.
// Shared by DebitCredits and DebitCreditsForPlatformLLM so both kinds get
// the identical atomicity guarantee — lock, check, decrement, all inside
// one transaction, same pattern as CompleteCreditTransaction.
func (s *Store) debitCredits(ctx context.Context, userID string, amountUSDMicros int64, insertLedger func(ctx context.Context, tx pgx.Tx) error) error {
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

	if err := insertLedger(ctx, tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ReserveCredits atomically checks a user's balance covers amountUSDMicros
// and, if so, immediately decrements it — without yet writing a debit_ledger
// row. x402 payments split the check from the real payment attempt by a
// network round trip (sign, relay, wait for settlement); reserving the
// balance up front, at the same atomic-decrement primitive DebitCredits
// already uses, closes the gap where concurrent or sequential calls within
// one node execution could all pass a check against the same stale balance.
// Pair with CommitReservedDebit once the payment is confirmed settled, or
// ReleaseReservedCredits if it never happened.
func (s *Store) ReserveCredits(ctx context.Context, userID string, amountUSDMicros int64) error {
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
		return fmt.Errorf("insufficient credits: balance %d micros, need %d micros: %w", balance, amountUSDMicros, ErrInsufficientCredits)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET credit_balance_usd_micros = credit_balance_usd_micros - $1 WHERE id = $2
	`, amountUSDMicros, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CommitReservedDebit records the debit_ledger audit row for a
// ReserveCredits reservation that turned into a real, settled charge. The
// balance was already decremented at reservation time, so this only writes
// the audit trail — it must never be called with an amount that wasn't
// already reserved.
func (s *Store) CommitReservedDebit(ctx context.Context, userID string, amountUSDMicros int64, kind, workflowID, runID, nodeID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO debit_ledger (user_id, workflow_id, run_id, node_id, kind, amount_usd_micros)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, workflowID, runID, nodeID, kind, amountUSDMicros)
	return err
}

// ReleaseReservedCredits credits back a ReserveCredits reservation that
// never became a real charge (the payment attempt failed, or was never
// confirmed settled, before any money moved). No debit_ledger row: nothing
// was ever actually charged, so there is nothing there to reverse.
func (s *Store) ReleaseReservedCredits(ctx context.Context, userID string, amountUSDMicros int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET credit_balance_usd_micros = credit_balance_usd_micros + $1 WHERE id = $2
	`, amountUSDMicros, userID)
	return err
}

// DebitCredits atomically charges a user's credit balance for a metered
// action inside a workflow run, and records the charge in debit_ledger.
func (s *Store) DebitCredits(ctx context.Context, userID string, amountUSDMicros int64, kind, workflowID, runID, nodeID string) error {
	return s.debitCredits(ctx, userID, amountUSDMicros, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO debit_ledger (user_id, workflow_id, run_id, node_id, kind, amount_usd_micros)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, userID, workflowID, runID, nodeID, kind, amountUSDMicros)
		return err
	})
}

// DebitCreditsForPlatformLLM is DebitCredits specialized for the
// platform_key_llm_fee kind: same atomic lock/check/decrement guarantee,
// plus the model and token counts captured for internal margin tracking —
// the charge is always the flat tier fee in amountUSDMicros regardless of
// actual token count, so these columns are informational, not billing.
func (s *Store) DebitCreditsForPlatformLLM(ctx context.Context, userID string, amountUSDMicros int64, workflowID, runID, nodeID, model string, tokensIn, tokensOut int) error {
	return s.debitCredits(ctx, userID, amountUSDMicros, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO debit_ledger (user_id, workflow_id, run_id, node_id, kind, amount_usd_micros, model, tokens_in, tokens_out)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, userID, workflowID, runID, nodeID, models.DebitKindPlatformKeyLLMFee, amountUSDMicros, model, tokensIn, tokensOut)
		return err
	})
}

// ListDebitLedger returns every debit_ledger row for a run, oldest first.
// Used by the credits/usage dashboard and by tests asserting exactly which
// charges a run produced.
func (s *Store) ListDebitLedger(ctx context.Context, runID string) ([]models.DebitEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, workflow_id, run_id, node_id, kind, amount_usd_micros, created_at, model, tokens_in, tokens_out
		FROM debit_ledger WHERE run_id = $1 ORDER BY created_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.DebitEntry
	for rows.Next() {
		var e models.DebitEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.WorkflowID, &e.RunID, &e.NodeID, &e.Kind, &e.AmountUSDMicros, &e.CreatedAt, &e.Model, &e.TokensIn, &e.TokensOut); err != nil {
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

// RecordRunFunding inserts one x402_run_fundings audit row for a real,
// already-settled inbound payment (Wallet 1 -> Wallet 2) that pre-funds a
// whole run's worth of downstream x402 tool calls, instead of one inbound
// settlement per call.
func (s *Store) RecordRunFunding(ctx context.Context, runID, inboundTxID string, amountAssetMicros int64) (models.X402RunFunding, error) {
	var f models.X402RunFunding
	err := s.pool.QueryRow(ctx, `
		INSERT INTO x402_run_fundings (run_id, inbound_tx_id, amount_asset_micros)
		VALUES ($1, $2, $3)
		RETURNING id, run_id, inbound_tx_id, amount_asset_micros, created_at
	`, runID, inboundTxID, amountAssetMicros).Scan(&f.ID, &f.RunID, &f.InboundTxID, &f.AmountAssetMicros, &f.CreatedAt)
	return f, err
}

// RecordRunFundedSettlement inserts an x402_relay_settlements audit row
// attributed to an existing run-level bulk settlement (run_funding_id)
// instead of a fresh per-call inbound one (inbound_tx_id). Takes
// amountAssetMicros directly at INSERT time — RecordOutboundSettlement only
// ever updates outbound_tx_id/status, never amount_asset_micros, so there is
// no later call that could backfill a placeholder value here.
// RecordInboundSettlement (the existing per-call equivalent) already sets
// the real amount at INSERT time for the same reason — this mirrors that,
// not a new pattern. status is left unset so it defaults to
// 'pending_outbound', matching RecordInboundSettlement's behavior;
// RecordOutboundSettlement's later call must pass "settled" or "failed" to
// satisfy the table's status CHECK constraint.
func (s *Store) RecordRunFundedSettlement(ctx context.Context, runFundingID, targetURL string, amountAssetMicros int64) (models.X402RelaySettlement, error) {
	var row models.X402RelaySettlement
	err := s.pool.QueryRow(ctx, `
		INSERT INTO x402_relay_settlements (target_url, run_funding_id, amount_asset_micros)
		VALUES ($1, $2, $3)
		RETURNING id, target_url, inbound_tx_id, outbound_tx_id, amount_asset_micros, status, created_at
	`, targetURL, runFundingID, amountAssetMicros).Scan(&row.ID, &row.TargetURL, &row.InboundTxID, &row.OutboundTxID, &row.AmountAssetMicros, &row.Status, &row.CreatedAt)
	return row, err
}

// ListX402RunFundingsByRun returns every x402_run_fundings row for a given
// run, oldest first. Used by tests asserting exactly one run-level pre-fund
// happened per agent run (Task 5's reserveAndFundRun).
func (s *Store) ListX402RunFundingsByRun(ctx context.Context, runID string) ([]models.X402RunFunding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, inbound_tx_id, amount_asset_micros, created_at
		FROM x402_run_fundings WHERE run_id = $1 ORDER BY created_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.X402RunFunding
	for rows.Next() {
		var row models.X402RunFunding
		if err := rows.Scan(&row.ID, &row.RunID, &row.InboundTxID, &row.AmountAssetMicros, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListX402RelaySettlementsByRunFunding returns every x402_relay_settlements
// row attributed to a given run-level bulk funding (run_funding_id), oldest
// first. Used by tests asserting exactly which per-call settlements a
// run-funded agent turn produced.
func (s *Store) ListX402RelaySettlementsByRunFunding(ctx context.Context, runFundingID string) ([]models.X402RelaySettlement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, target_url, inbound_tx_id, outbound_tx_id, amount_asset_micros, status, created_at
		FROM x402_relay_settlements WHERE run_funding_id = $1 ORDER BY created_at ASC
	`, runFundingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.X402RelaySettlement
	for rows.Next() {
		var row models.X402RelaySettlement
		if err := rows.Scan(&row.ID, &row.TargetURL, &row.InboundTxID, &row.OutboundTxID, &row.AmountAssetMicros, &row.Status, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

const tendrilLeaseCols = `id, user_id, workflow_id, run_id, node_id, lease_id,
	lease_token_enc, tendril_node_id, tendril_node_label, ssh_host, ssh_port,
	ssh_username, ssh_command, ssh_public_key, ssh_private_key_enc,
	ssh_password_enc, rate_usd_micros_per_hour, hours_purchased,
	reserved_usd_micros, charged_usd_micros, used_seconds, status, started_at,
	funded_until, released_at`

func scanTendrilLease(row pgx.Row) (models.TendrilLease, error) {
	var l models.TendrilLease
	err := row.Scan(&l.ID, &l.UserID, &l.WorkflowID, &l.RunID, &l.NodeID, &l.LeaseID,
		&l.LeaseTokenEnc, &l.TendrilNodeID, &l.TendrilNodeLabel, &l.SSHHost, &l.SSHPort,
		&l.SSHUsername, &l.SSHCommand, &l.SSHPublicKey, &l.SSHPrivateKeyEnc,
		&l.SSHPasswordEnc, &l.RateUSDMicrosPerHour, &l.HoursPurchased,
		&l.ReservedUSDMicros, &l.ChargedUSDMicros, &l.UsedSeconds, &l.Status,
		&l.StartedAt, &l.FundedUntil, &l.ReleasedAt)
	return l, err
}

func (s *Store) InsertTendrilLease(ctx context.Context, l models.TendrilLease) (models.TendrilLease, error) {
	return scanTendrilLease(s.pool.QueryRow(ctx, `
		INSERT INTO tendril_leases (user_id, workflow_id, run_id, node_id, lease_id,
			lease_token_enc, tendril_node_id, tendril_node_label, ssh_host, ssh_port,
			ssh_username, ssh_command, ssh_public_key, ssh_private_key_enc,
			ssh_password_enc, rate_usd_micros_per_hour, hours_purchased,
			reserved_usd_micros, funded_until)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING `+tendrilLeaseCols,
		l.UserID, l.WorkflowID, l.RunID, l.NodeID, l.LeaseID, l.LeaseTokenEnc,
		l.TendrilNodeID, l.TendrilNodeLabel, l.SSHHost, l.SSHPort, l.SSHUsername,
		l.SSHCommand, l.SSHPublicKey, l.SSHPrivateKeyEnc, l.SSHPasswordEnc,
		l.RateUSDMicrosPerHour, l.HoursPurchased, l.ReservedUSDMicros, l.FundedUntil))
}

func (s *Store) GetTendrilLease(ctx context.Context, id string) (models.TendrilLease, error) {
	return scanTendrilLease(s.pool.QueryRow(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases WHERE id = $1`, id))
}

func (s *Store) ListActiveTendrilLeases(ctx context.Context, userID string) ([]models.TendrilLease, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases
		 WHERE user_id = $1 AND status = 'active' ORDER BY started_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TendrilLease
	for rows.Next() {
		l, err := scanTendrilLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListExpiredTendrilLeases feeds the reaper. Reaps on started_at +
// hours_purchased -- the window THIS renter actually paid for -- not
// funded_until, which reflects how long Tendril's shared pool wallet (every
// AgentMesh user's topups combined) could fund the machine at its rate.
// funded_until is almost always far more than any one user bought (confirmed
// live: a 1-hour rent showed a ~2-hour funded_until), so reaping on it let a
// renter keep metering against the shared pool, for free, well past their
// own paid window -- worse the healthier the pool's balance, since that's
// exactly what stretches funded_until further from what was actually paid
// for. hours_purchased is fractional (a 0.5-hour rent is valid), hence the
// interval multiplication rather than an integer add.
func (s *Store) ListExpiredTendrilLeases(ctx context.Context, now time.Time) ([]models.TendrilLease, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases
		 WHERE status = 'active' AND started_at + (hours_purchased * interval '1 hour') <= $1
		 ORDER BY started_at + (hours_purchased * interval '1 hour')`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TendrilLease
	for rows.Next() {
		l, err := scanTendrilLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// MarkTendrilLeaseReleased transitions a lease from active to released, and
// reports via the bool whether THIS call performed a genuine transition (as
// opposed to a no-op update against a row some other caller already closed
// -- concurrent release attempts, or the reaper racing a user's own click).
// A caller that refunded an unused reservation without checking this bool
// used to double-refund on that race, or report a refund that never
// happened when Tendril's own watchdog closed the lease first (a 404 from
// Tendril has no charged amount for us to reconcile against).
func (s *Store) MarkTendrilLeaseReleased(ctx context.Context, id string, usedSeconds, chargedUSDMicros int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tendril_leases
		   SET status = 'released', released_at = NOW(),
		       used_seconds = $2, charged_usd_micros = $3
		 WHERE id = $1 AND status = 'active'`, id, usedSeconds, chargedUSDMicros)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// LatestActiveLeaseForRun is how a run/release node finds the lease its own
// run opened, without the canvas having to thread an id between nodes.
func (s *Store) LatestActiveLeaseForRun(ctx context.Context, runID string) (models.TendrilLease, error) {
	return scanTendrilLease(s.pool.QueryRow(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases
		 WHERE run_id = $1 AND status = 'active'
		 ORDER BY started_at DESC LIMIT 1`, runID))
}

// LatestActiveLeaseForUser is the fallback resolveLease reaches for once a
// Run/Release step is split into its own standalone one-node workflow: its
// own run_id never matches the Rent step's (that was a different run
// entirely), so the only thing left to resolve against is "whichever
// machine this user currently has open" — still scoped to one user, never
// the shared pool, so it can never resolve to someone else's lease.
func (s *Store) LatestActiveLeaseForUser(ctx context.Context, userID string) (models.TendrilLease, error) {
	return scanTendrilLease(s.pool.QueryRow(ctx,
		`SELECT `+tendrilLeaseCols+` FROM tendril_leases
		 WHERE user_id = $1 AND status = 'active'
		 ORDER BY started_at DESC LIMIT 1`, userID))
}
