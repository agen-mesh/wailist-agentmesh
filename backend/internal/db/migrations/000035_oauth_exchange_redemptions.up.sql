CREATE TABLE oauth_exchange_redemptions (
    code_hash BYTEA PRIMARY KEY CHECK (octet_length(code_hash) = 32),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX oauth_exchange_redemptions_expiry ON oauth_exchange_redemptions (expires_at);

-- Only the backend database role may consume codes.
ALTER TABLE oauth_exchange_redemptions ENABLE ROW LEVEL SECURITY;
