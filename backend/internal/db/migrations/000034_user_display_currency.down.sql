ALTER TABLE user_settings DROP CONSTRAINT IF EXISTS user_settings_display_currency_valid;
ALTER TABLE user_settings DROP COLUMN IF EXISTS display_currency;
