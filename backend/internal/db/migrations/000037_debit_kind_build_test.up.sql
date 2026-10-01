-- A build's test run executes the user's platform-key agents for real, so it
-- must be charged for real. It has no runs row to point at (nothing is
-- persisted about a test run), so run_id becomes nullable and the new kind
-- carries the charge on its own.
ALTER TABLE debit_ledger ALTER COLUMN run_id DROP NOT NULL;

ALTER TABLE debit_ledger DROP CONSTRAINT IF EXISTS debit_ledger_kind_valid;
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_kind_valid
    CHECK (kind IN ('byok_flat_fee', 'x402_platform_fee', 'x402_relay_cost',
                    'platform_key_llm_fee', 'tendril_lease', 'build_test_llm_fee'));

-- Every kind except the new one still belongs to a run.
ALTER TABLE debit_ledger ADD CONSTRAINT debit_ledger_run_required
    CHECK (run_id IS NOT NULL OR kind = 'build_test_llm_fee');
