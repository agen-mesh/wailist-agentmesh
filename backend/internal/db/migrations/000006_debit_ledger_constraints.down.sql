ALTER TABLE debit_ledger DROP CONSTRAINT IF EXISTS debit_ledger_amount_positive;
ALTER TABLE debit_ledger DROP CONSTRAINT IF EXISTS debit_ledger_kind_valid;
