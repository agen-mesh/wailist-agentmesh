-- A shareable snapshot of one workflow's graph: what sits behind a share
-- link (/s/<token>) and behind the paste-code the Share dialog offers.
--
-- The graph is a FROZEN COPY, not a pointer back to workflows.graph, and
-- that is the whole design. A link that served the live row would publish
-- every later edit retroactively to everyone already holding it -- paste a
-- real API key into the canvas tomorrow and the sanitiser becomes the only
-- thing standing between that key and every recipient. Freezing it at share
-- time also makes revocation mean something: there is one row to kill, and
-- killing it cannot be undone by the next save.
--
-- What lands in `graph` has already been through handlers.SanitizeGraphForShare,
-- which rebuilds each node from an allowlist of portable fields. Nothing
-- encrypted, nothing owner-bound and no uploaded file bytes reach this table,
-- so a row here is safe to serve to an unauthenticated reader.
CREATE TABLE IF NOT EXISTS workflow_shares (
    -- crypto/rand via handlers.randURLSafe(16) -> 22 base64url chars. Stored
    -- in the clear, unlike oauth_exchange_redemptions.code_hash, and the
    -- difference is deliberate: that table guards an OAuth code, while this
    -- one guards a snapshot its owner chose to publish. Keeping the token
    -- readable is what lets "Manage links" show a sharer the link again
    -- instead of making them mint a new one and re-send it.
    token        TEXT PRIMARY KEY CHECK (char_length(token) BETWEEN 16 AND 64),

    -- Provenance, and the reason a delete retracts: removing a workflow
    -- takes its links with it, which is what a person deleting something
    -- expects. The snapshot does not need this row to be served -- it is
    -- self-contained -- so the cascade is a policy choice, not a dependency.
    workflow_id  TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,

    -- Who may revoke and list. Denormalised from workflows.user_id so a
    -- sharer's own links can be listed without joining, and so the row still
    -- answers "whose was this" while the cascade above is running.
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Copied at share time rather than read through workflow_id, for the same
    -- frozen-snapshot reason as graph: renaming the workflow later must not
    -- silently retitle a link somebody has already passed on.
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),

    -- Mirrored by handlers.maxShareGraphBytes. 1 MiB is generous for a graph
    -- whose uploaded file bytes have been stripped -- the largest sample
    -- workflow in this repo is well under 30 KiB -- while still bounding what
    -- one row can cost to store and to serve on a public route.
    graph        JSONB NOT NULL CHECK (octet_length(graph::text) <= 1048576),

    -- Denormalised so the preview can say "12 nodes, 14 edges" without
    -- parsing a megabyte of JSONB, and so listing a user's links stays cheap.
    -- Written from the sanitised graph, never from client input.
    node_count   INT NOT NULL DEFAULT 0 CHECK (node_count >= 0),
    edge_count   INT NOT NULL DEFAULT 0 CHECK (edge_count >= 0),

    -- How many workspaces this link has been imported into. Shown to the
    -- sharer; never exposed on the public read.
    import_count INT NOT NULL DEFAULT 0 CHECK (import_count >= 0),

    -- NULL means the link never expires, which is the default the dialog
    -- offers. A past value reads exactly like a revoked row: 404.
    expires_at   TIMESTAMPTZ,

    -- Soft delete. The row is kept rather than deleted so import_count and
    -- created_at survive for the sharer's own listing, and so a revoked
    -- token can never be re-minted onto a different snapshot.
    revoked_at   TIMESTAMPTZ,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The sharer's own listing: their links, newest first.
CREATE INDEX IF NOT EXISTS idx_workflow_shares_user ON workflow_shares (user_id, created_at DESC);

-- "Which links exist for this workflow", which the Share dialog asks every
-- time it opens.
CREATE INDEX IF NOT EXISTS idx_workflow_shares_workflow ON workflow_shares (workflow_id);
