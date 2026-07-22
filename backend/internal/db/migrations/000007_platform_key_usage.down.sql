ALTER TABLE debit_ledger DROP CONSTRAINT debit_ledger_kind_valid;
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_kind_valid
    CHECK (kind IN ('byok_flat_fee', 'x402_platform_fee'));

ALTER TABLE debit_ledger DROP COLUMN tokens_out;
ALTER TABLE debit_ledger DROP COLUMN tokens_in;
ALTER TABLE debit_ledger DROP COLUMN model;
