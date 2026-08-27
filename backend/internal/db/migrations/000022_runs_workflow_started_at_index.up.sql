-- CreateRunWithCooldown's per-trigger hot path runs:
--   SELECT started_at FROM runs WHERE workflow_id = $1 ORDER BY started_at DESC LIMIT 1
-- while holding an exclusive advisory lock. The pre-existing idx_runs_workflow_id
-- lets Postgres filter by workflow_id but still has to sort every matching row to
-- find the latest one -- an unbounded scan for a long-lived, frequently-triggered
-- workflow, run on every single trigger while holding the lock. This composite
-- index gives it an index-only path straight to the most recent row.
CREATE INDEX idx_runs_workflow_id_started_at ON runs (workflow_id, started_at DESC);
