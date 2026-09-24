-- A short, human description of what a workflow does, shown on its detail
-- screen. Nullable: nothing is backfilled, and the app falls back to a
-- summary derived from the graph while it is empty.
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS description TEXT
    CHECK (char_length(description) <= 2000);
