-- Crypto top-ups (NOWPayments) quote directly in USD, so they have no INR amount or
-- FX rate — relax those to nullable and add a USD-cents column for this provider family.
ALTER TABLE credit_ledger ALTER COLUMN amount_inr_paise DROP NOT NULL;
ALTER TABLE credit_ledger ALTER COLUMN fx_rate_usd_per_inr DROP NOT NULL;
ALTER TABLE credit_ledger ADD COLUMN IF NOT EXISTS amount_usd_cents BIGINT;
ALTER TABLE credit_ledger ADD CONSTRAINT credit_ledger_amount_present
    CHECK (amount_inr_paise IS NOT NULL OR amount_usd_cents IS NOT NULL);
