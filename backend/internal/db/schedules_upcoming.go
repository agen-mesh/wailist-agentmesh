package db

import (
	"context"

	"github.com/agentmesh/backend/internal/models"
)

// ListScheduledWorkflows returns a user's workflows that the scheduler will
// fire: deployed, with a schedule and a next run. The same conditions
// ClaimDueSchedules claims on, so nothing listed here can fail to fire for
// being in the wrong state. Soonest first.
func (s *Store) ListScheduledWorkflows(ctx context.Context, userID string) ([]models.Workflow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+workflowColumns+`
		FROM workflows
		WHERE user_id = $1 AND NOT is_system AND status = 'deployed'
		  AND schedule_cron IS NOT NULL AND schedule_next_run_at IS NOT NULL
		ORDER BY schedule_next_run_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var wfs []models.Workflow
	for rows.Next() {
		w, err := scanWorkflowRow(rows)
		if err != nil {
			return nil, err
		}
		wfs = append(wfs, w)
	}
	return wfs, rows.Err()
}
