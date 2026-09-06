-- Which currency the UI renders amounts in. Presentation only: every stored
-- amount stays USD micros, x402 still settles in USDC, and Cashfree still
-- charges INR. Nothing about how money moves depends on this column.
--
-- DEFAULT 'USD' is load-bearing. It is what makes the feature opt-in: every
-- existing account keeps rendering exactly as it does today with no backfill,
-- and the frontend short-circuits before any rate lookup while this is 'USD'.
ALTER TABLE user_settings
    ADD COLUMN IF NOT EXISTS display_currency TEXT NOT NULL DEFAULT 'USD';

-- Same approach as 000033's CHECK constraints: an unsupported code becomes a
-- database error rather than a render-time crash somewhere in the UI. The list
-- is the curated shortlist the frontend offers; adding one later is a one-line
-- migration.
ALTER TABLE user_settings ADD CONSTRAINT user_settings_display_currency_valid
    CHECK (display_currency IN
        ('USD', 'INR', 'EUR', 'GBP', 'JPY', 'AUD', 'CAD', 'SGD', 'AED', 'CHF'));
