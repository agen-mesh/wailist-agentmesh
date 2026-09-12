package db

import (
	"context"
	"fmt"
	"time"
)

// MaxBuildMessageChars matches the CHECK on workflow_build_messages.text.
// Enforced here as a truncation rather than left to the database, which
// would reject the insert: losing a turn outright puts the conversation out
// of step with what the user actually said, which is worse than losing the
// tail of one long message.
const MaxBuildMessageChars = 16384

// DefaultBuildHistoryTurns is how many prior turns the builder replays. Far
// below any context limit on purpose -- the graph itself is also serialised
// into every call, and the useful signal ("the specs I gave you earlier")
// lives in the recent turns, not in the whole history of the workflow.
const DefaultBuildHistoryTurns = 20

// BuildMessage is one turn of the workflow builder's conversation. Role is
// "user" or "model", matching Gemini's own content roles so the replay needs
// no translation.
type BuildMessage struct {
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

// truncateChars cuts s to at most max characters, on a character boundary.
//
// Measured in runes, not bytes, for two reasons that both bite: the column's
// CHECK counts characters (char_length), so a byte cap would truncate
// multi-byte text far shorter than it needs to; and slicing a Go string by
// bytes can split a multi-byte rune in half, leaving invalid UTF-8 that
// Postgres rejects outright -- the very insert failure this exists to avoid.
func truncateChars(s string, max int) string {
	if len(s) <= max {
		return s // byte length bounds rune count, so this is already short enough
	}
	n := 0
	for i := range s {
		if n == max {
			return s[:i]
		}
		n++
	}
	return s
}

// AppendBuildMessage records one turn.
func (s *Store) AppendBuildMessage(ctx context.Context, workflowID, role, text string) error {
	if role != "user" && role != "model" {
		return fmt.Errorf("build message role must be \"user\" or \"model\", got %q", role)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workflow_build_messages (workflow_id, role, text)
		VALUES ($1, $2, $3)
	`, workflowID, role, truncateChars(text, MaxBuildMessageChars))
	return err
}

// GetBuildMessages returns the most recent `limit` turns, oldest first.
//
// The two orderings are deliberate: the subquery takes the NEWEST rows
// (id DESC LIMIT n), then the outer query puts those back into chronological
// order. Ordering ascending and limiting would return the OLDEST n instead --
// the opposite of what a conversation needs.
func (s *Store) GetBuildMessages(ctx context.Context, workflowID string, limit int) ([]BuildMessage, error) {
	if limit <= 0 {
		limit = DefaultBuildHistoryTurns
	}
	rows, err := s.pool.Query(ctx, `
		SELECT role, text, created_at FROM (
			SELECT id, role, text, created_at
			FROM workflow_build_messages
			WHERE workflow_id = $1
			ORDER BY id DESC
			LIMIT $2
		) recent
		ORDER BY id ASC
	`, workflowID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []BuildMessage{}
	for rows.Next() {
		var m BuildMessage
		if err := rows.Scan(&m.Role, &m.Text, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ClearBuildMessages drops the whole conversation for a workflow.
func (s *Store) ClearBuildMessages(ctx context.Context, workflowID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM workflow_build_messages WHERE workflow_id = $1`, workflowID)
	return err
}
