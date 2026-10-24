-- Attribution starts at creation. Historical rows remain unknown: ownership is
-- not evidence of who originally created an account.
ALTER TABLE accounts
 ADD COLUMN IF NOT EXISTS created_by BIGINT,
 ADD COLUMN IF NOT EXISTS created_by_name TEXT NOT NULL DEFAULT '',
 ADD COLUMN IF NOT EXISTS created_by_role TEXT NOT NULL DEFAULT '';

CREATE OR REPLACE FUNCTION accounts_keep_creator_snapshot() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.created_by,NEW.created_by_name,NEW.created_by_role)
    IS DISTINCT FROM (OLD.created_by,OLD.created_by_name,OLD.created_by_role) THEN
   RAISE EXCEPTION 'account creator snapshot is immutable';
 END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS accounts_creator_snapshot_immutable ON accounts;
CREATE TRIGGER accounts_creator_snapshot_immutable
 BEFORE UPDATE OF created_by,created_by_name,created_by_role ON accounts
 FOR EACH ROW EXECUTE FUNCTION accounts_keep_creator_snapshot();

ALTER TABLE accounts ALTER COLUMN concurrency SET DEFAULT 10;
ALTER TABLE accounts ALTER COLUMN priority SET DEFAULT 1;
-- Do not rewrite existing account settings or infer historical creators.
-- vendors.default_* columns are retained historical data, unused by this version.
