ALTER TABLE credit_ledger DROP CONSTRAINT IF EXISTS credit_ledger_amount_present;
ALTER TABLE credit_ledger DROP COLUMN IF EXISTS amount_usd_cents;
ALTER TABLE credit_ledger ALTER COLUMN fx_rate_usd_per_inr SET NOT NULL;
ALTER TABLE credit_ledger ALTER COLUMN amount_inr_paise SET NOT NULL;
