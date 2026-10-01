-- The workflow builder's own conversation, one row per turn.
--
-- Separate from run_logs and from the frontend's localStorage chat session:
-- this is what gets replayed into the model's context on the next build
-- turn, so it must survive a reload and follow the user across devices.
--
-- ON DELETE CASCADE mirrors workflow_variables: the conversation has no
-- meaning without the workflow it edits.
CREATE TABLE IF NOT EXISTS workflow_build_messages (
    id          BIGSERIAL PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('user', 'model')),
    text        TEXT NOT NULL CHECK (char_length(text) <= 16384),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The only read is "the most recent N turns for this workflow, in order".
CREATE INDEX IF NOT EXISTS workflow_build_messages_lookup
    ON workflow_build_messages (workflow_id, id DESC);
