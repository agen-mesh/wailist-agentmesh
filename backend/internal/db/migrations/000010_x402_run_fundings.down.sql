-- Restoring inbound_tx_id NOT NULL fails if any run-funded rows exist with a
-- genuinely null inbound_tx_id -- correct, matches the existing 000009
-- down-migration's documented pattern for a production audit ledger: a
-- down-migration that would silently drop or corrupt real audit data should
-- fail loudly instead.
ALTER TABLE x402_relay_settlements DROP CONSTRAINT x402_relay_settlements_funding_check;
ALTER TABLE x402_relay_settlements DROP COLUMN run_funding_id;
ALTER TABLE x402_relay_settlements ALTER COLUMN inbound_tx_id SET NOT NULL;

DROP TABLE IF EXISTS x402_run_fundings;
