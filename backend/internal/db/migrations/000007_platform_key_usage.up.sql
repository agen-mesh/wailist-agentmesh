ALTER TABLE debit_ledger ADD COLUMN model TEXT;
ALTER TABLE debit_ledger ADD COLUMN tokens_in INT;
ALTER TABLE debit_ledger ADD COLUMN tokens_out INT;

ALTER TABLE debit_ledger DROP CONSTRAINT debit_ledger_kind_valid;
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_kind_valid
    CHECK (kind IN ('byok_flat_fee', 'x402_platform_fee', 'platform_key_llm_fee'));
