-- Marks a workflow row as backing a partner console (Tendril, Prism) rather
-- than something a user built. GetOrCreateSystemWorkflow sets this true on
-- INSERT; FindSystemWorkflow now requires it on lookup instead of matching
-- on name alone.
--
-- Name-only matching had a real hijack path: UpdateWorkflow never validated
-- names, so a user renaming their OWN workflow to exactly
-- "Tendril Console (managed — do not edit)" -- and FindSystemWorkflow's
-- ORDER BY created_at ASC LIMIT 1 picks the OLDEST match -- made that real
-- workflow BE the console from then on: inaccessible via the normal canvas
-- editor (WorkflowRoute dispatches on id), with console runs/spend
-- attributed to it. is_system makes identity a real column a rename can
-- never touch, not a string a user's own input can spoof.
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS is_system BOOLEAN NOT NULL DEFAULT false;

-- Backfill: a console row created before this column existed must be marked
-- retroactively, or it silently loses its identity as of this migration --
-- GetOrCreateSystemWorkflow would mint a SECOND row for that user on next
-- use, orphaning the first (and its run/spend history) under a name that no
-- longer resolves to anything.
UPDATE workflows SET is_system = true
    WHERE name IN ('Tendril Console (managed — do not edit)', 'Prism Console (managed, do not edit)');

-- ListWorkflows filters WHERE NOT is_system so a partner console never shows
-- up next to a user's real workflows. Every user's list is small, but there
-- is no reason to make that a sequential predicate over an unindexed column
-- when a partial index (same pattern as idx_workflows_geofence_enabled,
-- migration 000029) covers exactly the query it runs.
CREATE INDEX IF NOT EXISTS idx_workflows_user_visible
    ON workflows (user_id, updated_at DESC)
    WHERE NOT is_system;
