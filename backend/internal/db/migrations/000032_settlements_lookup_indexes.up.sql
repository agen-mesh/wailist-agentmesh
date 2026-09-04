-- Indexes for ListSettlements (usage.go), which walks run_logs -> runs ->
-- workflows to find the settlement receipts belonging to one user.
--
-- Without idx_workflows_user_id that join starts from a sequential scan of
-- workflows on every /usage/settlements request. The endpoint's ?limit= caps
-- the rows returned but not the work done: the DISTINCT ON that de-duplicates
-- a run-funded run's repeated tx id has to see every candidate row before any
-- limit can apply, so this read grows with a user's total run history rather
-- than with the page size.
CREATE INDEX IF NOT EXISTS idx_workflows_user_id ON workflows (user_id);

-- Serves the same query's ordering step. run_logs already has (run_id,
-- step_index) and (run_id, node_id, ts DESC), neither of which helps a scan
-- that arrives by run_id and then sorts the whole set by ts.
CREATE INDEX IF NOT EXISTS idx_run_logs_run_id_ts ON run_logs (run_id, ts DESC);
