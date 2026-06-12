CREATE TABLE IF NOT EXISTS published_workflows (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    creator_id    TEXT NOT NULL REFERENCES users(id),
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    tags          TEXT[] NOT NULL DEFAULT '{}',
    graph         JSONB NOT NULL DEFAULT '{"nodes":[],"edges":[]}',
    fee_per_run   DECIMAL(10,6) NOT NULL DEFAULT 0,
    run_count     INT NOT NULL DEFAULT 0,
    upvote_count  INT NOT NULL DEFAULT 0,
    published_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS published_workflow_votes (
    user_id     TEXT NOT NULL REFERENCES users(id),
    workflow_id TEXT NOT NULL REFERENCES published_workflows(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, workflow_id)
);

ALTER TABLE workflows ADD COLUMN IF NOT EXISTS source_published_id TEXT REFERENCES published_workflows(id);

CREATE INDEX IF NOT EXISTS idx_published_workflows_rank
    ON published_workflows (upvote_count DESC, run_count DESC);
CREATE INDEX IF NOT EXISTS idx_published_workflows_creator
    ON published_workflows (creator_id);
CREATE INDEX IF NOT EXISTS idx_workflows_source_published
    ON workflows (source_published_id) WHERE source_published_id IS NOT NULL;
