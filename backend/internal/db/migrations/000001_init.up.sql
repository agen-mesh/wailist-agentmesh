CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workflows (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL DEFAULT 'dev',
    name         TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'draft',
    graph        JSONB NOT NULL DEFAULT '{"nodes":[],"edges":[]}',
    deployed_at  TIMESTAMPTZ,
    run_endpoint TEXT,
    notify_url   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_wallets (
    id                  TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workflow_id         TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    agent_node_id       TEXT NOT NULL,
    address             TEXT NOT NULL,
    encrypted_mnemonic  TEXT NOT NULL,
    network             TEXT NOT NULL DEFAULT 'testnet',
    UNIQUE (workflow_id, agent_node_id)
);

CREATE TABLE IF NOT EXISTS tool_credentials (
    id                TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id           TEXT NOT NULL DEFAULT 'dev',
    provider          TEXT NOT NULL,
    encrypted_api_key TEXT NOT NULL,
    UNIQUE (user_id, provider)
);

CREATE TABLE IF NOT EXISTS runs (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workflow_id   TEXT NOT NULL REFERENCES workflows(id),
    triggered_by  TEXT NOT NULL DEFAULT 'manual',
    status        TEXT NOT NULL DEFAULT 'running',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ,
    input_context JSONB
);

CREATE TABLE IF NOT EXISTS run_logs (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    run_id      TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_index  INT NOT NULL,
    node_id     TEXT NOT NULL,
    node_type   TEXT NOT NULL,
    status      TEXT NOT NULL,
    input       JSONB,
    output      JSONB,
    duration_ms INT,
    ts          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_run_logs_run_id ON run_logs (run_id, step_index);
CREATE INDEX IF NOT EXISTS idx_runs_workflow_id ON runs (workflow_id);
