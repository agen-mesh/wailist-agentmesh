-- Per-user account settings, edited from the /settings page.
--
-- One row per user, created lazily on first write: GetUserSettings returns
-- defaults for a user with no row rather than 404ing, so signup does not have
-- to remember to seed this table and existing accounts keep working untouched.
--
-- The CHECK constraints are the safety property. They make an out-of-range
-- value a database error rather than something every future call site has to
-- remember to validate — the same approach 000017's tendril_credit_non_negative
-- takes for the Tendril balance.
CREATE TABLE IF NOT EXISTS user_settings (
    user_id                    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- Balance below which the UI warns. Default matches the frontend's
    -- previous hardcoded $5 (lib/credits/store.ts, app/billing/page.tsx).
    low_balance_usd_micros     BIGINT NOT NULL DEFAULT 5000000,
    -- Optional per-call spend ceiling, enforced in engine.Runner.preflightCheck.
    -- NULL means "no user ceiling" — the global MaxSingleX402QuoteUSDMicros
    -- still applies, so NULL is a weaker limit, never an unlimited one.
    max_call_spend_usd_micros  BIGINT,
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_settings_low_balance_non_negative
        CHECK (low_balance_usd_micros >= 0),
    -- Not merely "> 0": Runner.preflightCheck skips its settings lookup for any
    -- amount at or below models.X402ProbeFloorUSDMicros ($0.05), which is only
    -- sound if no stored ceiling can sit beneath that floor. parseSettingsPatch
    -- validates the same bound; this constraint is what makes it true for every
    -- other writer.
    CONSTRAINT user_settings_max_call_spend_above_probe_floor
        CHECK (max_call_spend_usd_micros IS NULL OR max_call_spend_usd_micros >= 50000)
);
