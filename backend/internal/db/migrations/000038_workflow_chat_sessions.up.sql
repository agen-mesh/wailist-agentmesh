-- The console chat transcript for one workflow, as the canvas shows it.
--
-- Previously kept in the browser's localStorage, which meant the
-- conversation was per-browser: it vanished on sign-out or a device change,
-- and a turn stranded mid-run could only be recovered in the same browser.
-- One row per workflow, holding the whole transcript, mirroring what the
-- client used to store under one localStorage key.
CREATE TABLE IF NOT EXISTS workflow_chat_sessions (
    workflow_id TEXT PRIMARY KEY REFERENCES workflows(id) ON DELETE CASCADE,
    session_id  TEXT NOT NULL,
    messages    JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
