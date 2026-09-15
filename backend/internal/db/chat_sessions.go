package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MaxChatTranscriptBytes bounds one stored transcript.
const MaxChatTranscriptBytes = 512 * 1024

// ErrInvalidTranscript separates a client mistake (400) from a database
// failure (500).
var ErrInvalidTranscript = errors.New("invalid chat transcript")

// ChatMode is which conversation a transcript belongs to: building the
// workflow, or talking to the finished one.
type ChatMode string

const (
	ChatModeBuild ChatMode = "build"
	ChatModeRun   ChatMode = "run"
)

// ParseChatMode accepts the two modes and nothing else, defaulting to run.
func ParseChatMode(s string) (ChatMode, bool) {
	switch ChatMode(s) {
	case ChatModeBuild:
		return ChatModeBuild, true
	case ChatModeRun, "":
		return ChatModeRun, true
	}
	return "", false
}

// ChatSession is one workflow's console transcript.
type ChatSession struct {
	SessionID string          `json:"sessionId"`
	Messages  json.RawMessage `json:"messages"`
}

// GetChatSession returns the transcript, or an empty one if there is none.
func (s *Store) GetChatSession(ctx context.Context, workflowID string, mode ChatMode) (ChatSession, error) {
	var out ChatSession
	err := s.pool.QueryRow(ctx, `
		SELECT session_id, messages FROM workflow_chat_sessions
		WHERE workflow_id = $1 AND mode = $2
	`, workflowID, string(mode)).Scan(&out.SessionID, &out.Messages)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatSession{Messages: json.RawMessage("[]")}, nil
	}
	if err != nil {
		return ChatSession{}, err
	}
	return out, nil
}

// SaveChatSession replaces the transcript; the client sends it whole.
func (s *Store) SaveChatSession(ctx context.Context, workflowID string, mode ChatMode, sessionID string, messages json.RawMessage) error {
	if len(messages) > MaxChatTranscriptBytes {
		return fmt.Errorf("%w: %d bytes, over the %d byte limit", ErrInvalidTranscript, len(messages), MaxChatTranscriptBytes)
	}
	var probe []json.RawMessage
	if err := json.Unmarshal(messages, &probe); err != nil {
		return fmt.Errorf("%w: it must be a JSON array of messages", ErrInvalidTranscript)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workflow_chat_sessions (workflow_id, mode, session_id, messages, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (workflow_id, mode) DO UPDATE
		SET session_id = EXCLUDED.session_id,
		    messages   = EXCLUDED.messages,
		    updated_at = NOW()
	`, workflowID, string(mode), sessionID, messages)
	return err
}
