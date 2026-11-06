CREATE TABLE arkiv_share_meter (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX arkiv_share_meter_user_created ON arkiv_share_meter (user_id, created_at);
CREATE INDEX arkiv_share_meter_created ON arkiv_share_meter (created_at);
