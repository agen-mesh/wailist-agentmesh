-- Push notification targets: one row per device that has registered for
-- notifications, owned by the user signed in on that device.
--
-- One TABLE rather than a column on users, because a person may carry more
-- than one device and each has its own FCM registration token. A column would
-- silently make the newest phone the only phone.
CREATE TABLE IF NOT EXISTS device_tokens (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- The FCM registration token. UNIQUE across the whole table, not per
    -- user, and that is the point: a token identifies a physical app install,
    -- not a person. Sign out on a phone and sign in as somebody else and FCM
    -- hands back the same token, so registration upserts and reassigns
    -- user_id. Without the global constraint the row would simply be
    -- duplicated and the previous account would keep receiving that device's
    -- notifications -- somebody else's runs, on somebody else's phone.
    token        TEXT NOT NULL UNIQUE,

    -- Free text, like runs.triggered_by. Only 'android' exists today; iOS has
    -- no app yet, and an enum would have to be migrated the day it does.
    platform     TEXT NOT NULL DEFAULT 'android',

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Touched on every re-registration, which the app does on each sign-in.
    -- FCM rotates tokens on its own schedule and never tells the server when
    -- one dies; this is what makes an abandoned row identifiable later
    -- without guessing. Nothing prunes on it yet -- removal today is driven by
    -- FCM rejecting a send, which is authoritative in a way a timestamp is not.
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The only lookup the send path makes: every token for one user.
CREATE INDEX IF NOT EXISTS idx_device_tokens_user ON device_tokens (user_id);
