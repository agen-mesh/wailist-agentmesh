ALTER TABLE users ADD COLUMN IF NOT EXISTS credit_balance_usd_micros BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS credit_ledger (
    id                   TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id              TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider             TEXT NOT NULL,
    provider_order_id    TEXT NOT NULL,
    provider_payment_id  TEXT,
    status               TEXT NOT NULL DEFAULT 'pending',
    amount_inr_paise     BIGINT NOT NULL,
    fx_rate_usd_per_inr  NUMERIC NOT NULL,
    credit_usd_micros    BIGINT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at         TIMESTAMPTZ,
    UNIQUE (provider, provider_order_id)
);

CREATE INDEX IF NOT EXISTS idx_credit_ledger_user ON credit_ledger(user_id);
