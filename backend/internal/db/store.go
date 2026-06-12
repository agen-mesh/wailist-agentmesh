package db

import (
	"context"
	"encoding/json"
	"errors"
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
		RETURNING id, email, password_hash, credits, created_at
	`, email, passwordHash).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Credits, &u.CreatedAt)
	return u, err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, credits, created_at
		FROM users WHERE email = $1
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Credits, &u.CreatedAt)
	return u, err
}

// GetOrCreateOAuthUser returns the user for a verified OAuth email, creating an
// OAuth-only account (empty password_hash, so bcrypt password login always fails)
// when none exists. Linking to an existing OAuth account by verified email is
// allowed; linking to a password account returns ErrPasswordAccountExists.
func (s *Store) GetOrCreateOAuthUser(ctx context.Context, email string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, credits, created_at FROM users WHERE email = $1
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Credits, &u.CreatedAt)
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
		RETURNING id, email, password_hash, credits, created_at
	`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Credits, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Lost a race: a row appeared between SELECT and INSERT. Re-fetch and
		// apply the same password-account guard.
		err = s.pool.QueryRow(ctx, `
			SELECT id, email, password_hash, credits, created_at FROM users WHERE email = $1
		`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Credits, &u.CreatedAt)
		if err == nil && u.PasswordHash != "" {
			return models.User{}, ErrPasswordAccountExists
		}
	}
	return u, err
}

// --- Credits / spend methods ---

func (s *Store) GetUserByID(ctx context.Context, userID string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, credits, created_at FROM users WHERE id=$1
	`, userID).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Credits, &u.CreatedAt)
	return u, err
}

func (s *Store) TopupCredits(ctx context.Context, userID string, amount float64) (float64, error) {
	var newBalance float64
	err := s.pool.QueryRow(ctx, `
		UPDATE users SET credits = credits + $2 WHERE id = $1 RETURNING credits
	`, userID, amount).Scan(&newBalance)
	return newBalance, err
}

func (s *Store) DeductCredits(ctx context.Context, userID string, amount float64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET credits = GREATEST(0, credits - $2) WHERE id = $1
	`, userID, amount)
	return err
}

func (s *Store) FinishRunWithCost(ctx context.Context, runID string, status models.RunStatus, cost float64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE runs SET status=$2, finished_at=NOW(), cost=$3 WHERE id=$1
	`, runID, string(status), cost)
	return err
}

// GetSpend returns total spend and spend in last 24h for a workflow's runs.
func (s *Store) GetSpend(ctx context.Context, workflowID string) (total, last24h float64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(CASE WHEN started_at > NOW() - INTERVAL '24 hours' THEN cost ELSE 0 END), 0)
		FROM runs
		WHERE workflow_id=$1 AND status='completed'
	`, workflowID).Scan(&total, &last24h)
	return
}

// GetUserSpend returns global total spend and 24h spend across all workflows for a user.
func (s *Store) GetUserSpend(ctx context.Context, userID string) (total, last24h float64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(r.cost), 0),
			COALESCE(SUM(CASE WHEN r.started_at > NOW() - INTERVAL '24 hours' THEN r.cost ELSE 0 END), 0)
		FROM runs r
		JOIN workflows w ON w.id = r.workflow_id
		WHERE w.user_id=$1 AND r.status='completed'
	`, userID).Scan(&total, &last24h)
	return
}

// GetLastAgentOutput returns the output of the last successful agent node in a run.
func (s *Store) GetLastAgentOutput(ctx context.Context, runID string) (string, error) {
	var out []byte
	err := s.pool.QueryRow(ctx, `
		SELECT output FROM run_logs
		WHERE run_id=$1 AND node_type='agent' AND status='success'
		ORDER BY step_index DESC, ts DESC LIMIT 1
	`, runID).Scan(&out)
	if err != nil {
		return "", err
	}
	var v any
	json.Unmarshal(out, &v)
	switch s := v.(type) {
	case string:
		return s, nil
	default:
		b, _ := json.Marshal(v)
		return string(b), nil
	}
}

// --- Waitlist methods ---

func (s *Store) InsertWaitlistEmail(ctx context.Context, email string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO waitlist (email) VALUES ($1) ON CONFLICT (email) DO NOTHING
	`, email)
	return err
}
