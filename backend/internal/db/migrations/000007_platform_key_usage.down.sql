ALTER TABLE debit_ledger DROP CONSTRAINT debit_ledger_kind_valid;
-- NOT VALID: skips re-validating existing rows, so this rollback doesn't
-- fail if any platform_key_llm_fee debit_ledger rows already exist. Their
-- kind stays as-is; only future inserts are checked against this constraint.
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_kind_valid
    CHECK (kind IN ('byok_flat_fee', 'x402_platform_fee')) NOT VALID;

ALTER TABLE debit_ledger DROP COLUMN tokens_out;
ALTER TABLE debit_ledger DROP COLUMN tokens_in;
ALTER TABLE debit_ledger DROP COLUMN model;
