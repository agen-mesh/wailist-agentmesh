ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_amount_positive CHECK (amount_usd_micros > 0);
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_kind_valid CHECK (kind IN ('byok_flat_fee', 'x402_platform_fee'));
