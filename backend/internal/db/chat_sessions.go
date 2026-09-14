package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MaxChatTranscriptBytes bounds one stored transcript. The client trims its
// own history well below this; the cap is here so a runaway transcript
// cannot grow a row without limit.
const MaxChatTranscriptBytes = 512 * 1024

// ErrInvalidTranscript marks a transcript the client got wrong, so the
// handler can answer 400 for those and 500 for a database failure.
var ErrInvalidTranscript = errors.New("invalid chat transcript")

// ChatSession is one workflow's console transcript.
type ChatSession struct {
	SessionID string          `json:"sessionId"`
	Messages  json.RawMessage `json:"messages"`
}

// GetChatSession returns the stored transcript, or an empty one for a
// workflow that has no conversation yet.
func (s *Store) GetChatSession(ctx context.Context, workflowID string) (ChatSession, error) {
	var out ChatSession
	err := s.pool.QueryRow(ctx, `
		SELECT session_id, messages FROM workflow_chat_sessions WHERE workflow_id = $1
	`, workflowID).Scan(&out.SessionID, &out.Messages)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatSession{Messages: json.RawMessage("[]")}, nil
	}
	if err != nil {
		return ChatSession{}, err
	}
	return out, nil
}

// SaveChatSession replaces the transcript. The client owns the whole
// transcript and sends it entire, exactly as it wrote one localStorage key.
func (s *Store) SaveChatSession(ctx context.Context, workflowID, sessionID string, messages json.RawMessage) error {
	if len(messages) > MaxChatTranscriptBytes {
		return fmt.Errorf("%w: %d bytes, over the %d byte limit", ErrInvalidTranscript, len(messages), MaxChatTranscriptBytes)
	}
	var probe []json.RawMessage
	if err := json.Unmarshal(messages, &probe); err != nil {
		return fmt.Errorf("%w: it must be a JSON array of messages", ErrInvalidTranscript)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workflow_chat_sessions (workflow_id, session_id, messages, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (workflow_id) DO UPDATE
		SET session_id = EXCLUDED.session_id,
		    messages   = EXCLUDED.messages,
		    updated_at = NOW()
	`, workflowID, sessionID, messages)
	return err
}
