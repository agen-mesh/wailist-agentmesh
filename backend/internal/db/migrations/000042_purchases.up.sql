BEGIN;

CREATE TABLE purchase_wallets (
    account_id text PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
    address text NOT NULL,
    verified_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (address)
);
CREATE TABLE purchase_wallet_challenges (
    id uuid PRIMARY KEY,
    account_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    payload jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz
);
CREATE INDEX purchase_wallet_challenges_account ON purchase_wallet_challenges(account_id);

CREATE TABLE purchase_allowances (
    account_id text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    wallet text NOT NULL,
    limit_micros bigint NOT NULL DEFAULT 0 CHECK (limit_micros >= 0),
    reserved_micros bigint NOT NULL DEFAULT 0 CHECK (reserved_micros >= 0),
    consumed_micros bigint NOT NULL DEFAULT 0 CHECK (consumed_micros >= 0),
    PRIMARY KEY(account_id,wallet),
    CHECK (reserved_micros::numeric + consumed_micros <= limit_micros)
);
CREATE TABLE purchase_orders (
    id uuid PRIMARY KEY,
    account_id text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_request_id text NOT NULL,
    intent_hash text NOT NULL UNIQUE,
    payer text NOT NULL,
    chain_id bigint NOT NULL CHECK (chain_id = 5042002),
    asset text NOT NULL CHECK (asset = '0x3600000000000000000000000000000000000000'),
    run_id text NOT NULL DEFAULT '',
    node_id text NOT NULL DEFAULT '',
    invocation text NOT NULL DEFAULT '',
    state text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(account_id,client_request_id),
    UNIQUE(chain_id,asset,payer,intent_hash),
    CHECK (state IN ('awaiting_identity','awaiting_approval','approved','authorization_released','submitting','payment_unknown','paid','fulfilling','completed','paid_tool_failed','fulfillment_unknown','denied','expired','cancelled'))
);
CREATE UNIQUE INDEX purchase_orders_invocation ON purchase_orders(run_id,node_id,invocation) WHERE run_id <> '';
CREATE INDEX purchase_orders_reconciliation ON purchase_orders(updated_at) WHERE state IN ('submitting','payment_unknown','paid','fulfilling','fulfillment_unknown');
CREATE TABLE purchase_payment_events (
    chain_id bigint NOT NULL,
    tx_hash text NOT NULL,
    log_index bigint NOT NULL CHECK (log_index >= 0),
    order_id uuid NOT NULL UNIQUE REFERENCES purchase_orders(id) ON DELETE RESTRICT,
    payload jsonb NOT NULL,
    PRIMARY KEY(chain_id,tx_hash,log_index)
);
CREATE TABLE purchase_relayer_nonces (
    chain_id bigint NOT NULL,
    relayer text NOT NULL,
    next_nonce bigint NOT NULL CHECK (next_nonce >= 0),
    PRIMARY KEY(chain_id,relayer)
);
CREATE TABLE purchase_identity_attempts (
    state_hash text PRIMARY KEY,
    id text NOT NULL UNIQUE,
    account_id text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    order_id uuid NOT NULL REFERENCES purchase_orders(id) ON DELETE RESTRICT,
    intent_hash text NOT NULL,
    sealed_payload text NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    completed_at timestamptz,
    cancelled_at timestamptz
);
CREATE INDEX purchase_identity_attempts_account ON purchase_identity_attempts(account_id);
CREATE INDEX purchase_identity_attempts_order ON purchase_identity_attempts(order_id);
CREATE TABLE purchase_identities (
    issuer text NOT NULL,
    subject_hash text NOT NULL,
    sealed_subject text NOT NULL,
    account_id text NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    PRIMARY KEY(issuer,subject_hash),
    UNIQUE(issuer,account_id)
);
CREATE INDEX purchase_identities_account ON purchase_identities(account_id);

CREATE TABLE purchase_merchant_orders (
    order_id uuid PRIMARY KEY,
    intent_hash text NOT NULL UNIQUE,
    payload jsonb NOT NULL,
    state text NOT NULL CHECK (state IN ('accepted','pending','started','completed','failed','unknown')),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE purchase_merchant_events (
    chain_id bigint NOT NULL,
    tx_hash text NOT NULL,
    log_index bigint NOT NULL CHECK (log_index >= 0),
    order_id uuid NOT NULL UNIQUE REFERENCES purchase_merchant_orders(order_id) ON DELETE RESTRICT,
    PRIMARY KEY(chain_id,tx_hash,log_index)
);

COMMIT;
