CREATE TABLE x402_relay_settlements (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_url           TEXT NOT NULL,
    inbound_tx_id        TEXT NOT NULL UNIQUE,
    outbound_tx_id       TEXT,
    amount_asset_micros  BIGINT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'pending_outbound'
                         CHECK (status IN ('pending_outbound', 'settled', 'failed')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_x402_relay_settlements_status ON x402_relay_settlements(status);
