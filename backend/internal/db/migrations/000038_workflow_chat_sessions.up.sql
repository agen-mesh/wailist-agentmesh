-- The console chat transcript, as the canvas shows it. Previously kept in
-- the browser's localStorage, which made it per-browser: gone on sign-out or
-- a device change, and a turn stranded mid-run could only be recovered in the
-- browser that started it.
--
-- One row per workflow AND mode: building a workflow and talking to the
-- finished one are two different conversations, and interleaving them in a
-- single transcript reads as nonsense.
CREATE TABLE IF NOT EXISTS workflow_chat_sessions (
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    mode        TEXT NOT NULL CHECK (mode IN ('build', 'run')),
    session_id  TEXT NOT NULL,
    messages    JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workflow_id, mode)
);
