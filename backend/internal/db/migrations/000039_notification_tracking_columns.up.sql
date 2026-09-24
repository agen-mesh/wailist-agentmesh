-- Tracks whether a low-balance push has already fired for this user, so the
-- crossing notifies once rather than on every debit while the balance stays
-- low. Cleared once the balance recovers back above the threshold.
ALTER TABLE users ADD COLUMN IF NOT EXISTS low_balance_notified_at TIMESTAMPTZ;

-- Tracks the schedule_next_run_at value a schedule's upcoming-run heads-up
-- was already sent for, so the same occurrence is not warned about twice.
-- Distinct from schedule_next_run_at itself: that column belongs exclusively
-- to ClaimDueSchedules.
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS schedule_warned_for TIMESTAMPTZ;
