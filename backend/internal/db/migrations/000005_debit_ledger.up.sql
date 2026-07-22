ALTER TABLE users ADD CONSTRAINT credit_balance_non_negative CHECK (credit_balance_usd_micros >= 0);

CREATE TABLE IF NOT EXISTS debit_ledger (
    id                TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workflow_id       TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    run_id            TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    node_id           TEXT NOT NULL,
    kind              TEXT NOT NULL,
    amount_usd_micros BIGINT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_debit_ledger_user ON debit_ledger(user_id);
CREATE INDEX IF NOT EXISTS idx_debit_ledger_run ON debit_ledger(run_id);
