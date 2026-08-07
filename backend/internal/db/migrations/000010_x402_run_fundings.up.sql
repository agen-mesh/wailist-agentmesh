CREATE TABLE x402_run_fundings (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id               TEXT NOT NULL,
    inbound_tx_id        TEXT NOT NULL UNIQUE,
    amount_asset_micros  BIGINT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE x402_relay_settlements
    ALTER COLUMN inbound_tx_id DROP NOT NULL,
    ADD COLUMN run_funding_id UUID REFERENCES x402_run_fundings(id);

-- A row funded by a run-level bulk settlement has no per-call inbound tx of
-- its own (inbound_tx_id NULL, run_funding_id set); a normal per-call row
-- (via the unmodified public relay) keeps requiring inbound_tx_id. This is
-- an AUDIT record, not an enforcement mechanism — nothing about this
-- constraint gates spending; the actual spending gate is that
-- PayTargetFromWallet2 is only reachable from the public relay handler
-- (unchanged) or from engine's own trusted, in-memory-pool-gated code —
-- never from an externally reachable, unauthenticated path.
ALTER TABLE x402_relay_settlements
    ADD CONSTRAINT x402_relay_settlements_funding_check
    CHECK (inbound_tx_id IS NOT NULL OR run_funding_id IS NOT NULL);
