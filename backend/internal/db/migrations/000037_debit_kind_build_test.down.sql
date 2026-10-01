-- Down only works while no build_test_llm_fee rows exist: they have no run to
-- attribute to, so re-adding NOT NULL would fail rather than lose the charge.
ALTER TABLE debit_ledger DROP CONSTRAINT IF EXISTS debit_ledger_run_required;

ALTER TABLE debit_ledger DROP CONSTRAINT IF EXISTS debit_ledger_kind_valid;
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_kind_valid
    CHECK (kind IN ('byok_flat_fee', 'x402_platform_fee', 'x402_relay_cost',
                    'platform_key_llm_fee', 'tendril_lease'));

ALTER TABLE debit_ledger ALTER COLUMN run_id SET NOT NULL;
