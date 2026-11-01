BEGIN;

-- 000042 shipped inside this pull request. Databases that already applied
-- the first draft still have the Arc testnet checks and no reconcile lease.
DO $$
DECLARE name text;
BEGIN
    FOR name IN
        SELECT con.conname
        FROM pg_constraint con
        JOIN pg_class rel ON rel.oid = con.conrelid
        WHERE rel.relname = 'purchase_orders'
          AND con.contype = 'c'
          AND (
              pg_get_constraintdef(con.oid) LIKE '%5042002%'
              OR pg_get_constraintdef(con.oid) LIKE '%0x3600000000000000000000000000000000000000%'
          )
    LOOP
        EXECUTE format('ALTER TABLE purchase_orders DROP CONSTRAINT %I', name);
    END LOOP;
END $$;

CREATE TABLE IF NOT EXISTS purchase_reconcile_lease (
    id int PRIMARY KEY CHECK (id = 1),
    holder text NOT NULL DEFAULT '',
    until timestamptz NOT NULL DEFAULT '-infinity'
);
INSERT INTO purchase_reconcile_lease (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

COMMIT;
