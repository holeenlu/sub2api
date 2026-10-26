-- Dedicated supplier identities. 275 is reserved for the main-worktree scheduler.
ALTER TABLE vendors ADD COLUMN IF NOT EXISTS tier SMALLINT NOT NULL DEFAULT 1;
ALTER TABLE vendors ADD COLUMN IF NOT EXISTS parent_vendor_id BIGINT REFERENCES vendors(id);
ALTER TABLE vendors ALTER COLUMN account_limit SET DEFAULT 1000;
ALTER TABLE vendors ALTER COLUMN proxy_limit SET DEFAULT 1000;
ALTER TABLE vendors ALTER COLUMN default_concurrency SET DEFAULT 15;
ALTER TABLE vendors ALTER COLUMN default_priority SET DEFAULT 1;
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='vendors'::regclass AND conname='vendors_hierarchy_check') THEN
  ALTER TABLE vendors ADD CONSTRAINT vendors_hierarchy_check CHECK ((tier=1 AND parent_vendor_id IS NULL) OR (tier=2 AND parent_vendor_id IS NOT NULL AND parent_vendor_id<>id));
 END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_vendors_parent ON vendors(parent_vendor_id) WHERE parent_vendor_id IS NOT NULL;

-- Only actual existing suppliers' ordinary login users change role. Never infer
-- relationships or alter administrators, unrelated customers, balances or usage.
WITH converted AS (
 UPDATE users u SET role='vendor_level1',session_generation=session_generation+1,
 restrict_public_groups=TRUE,updated_at=NOW()
 WHERE u.role='user' AND EXISTS(SELECT 1 FROM vendors v WHERE v.user_id=u.id AND v.tier=1)
 RETURNING u.id
), revoked_keys AS (
 UPDATE api_keys SET status='disabled',updated_at=NOW() WHERE user_id IN(SELECT id FROM converted) AND deleted_at IS NULL RETURNING id
), revoked_groups AS (
 DELETE FROM user_allowed_groups WHERE user_id IN(SELECT id FROM converted) RETURNING user_id
), revoked_subscriptions AS (
 UPDATE user_subscriptions SET status='expired',updated_at=NOW() WHERE user_id IN(SELECT id FROM converted) AND status='active' RETURNING id
)
UPDATE pending_auth_sessions SET consumed_at=NOW() WHERE target_user_id IN(SELECT id FROM converted) AND consumed_at IS NULL;
-- Existing auth-cache outbox triggers invalidate changed user/key snapshots;
-- session_generation invalidates both access and refresh tokens, including MFA.

CREATE OR REPLACE FUNCTION check_vendor_hierarchy() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND (NEW.tier IS DISTINCT FROM OLD.tier OR NEW.parent_vendor_id IS DISTINCT FROM OLD.parent_vendor_id OR NEW.user_id IS DISTINCT FROM OLD.user_id) THEN
  RAISE EXCEPTION 'supplier hierarchy and login ownership are immutable';
 END IF;
 IF TG_OP='INSERT' AND NEW.tier=2 THEN
  PERFORM 1 FROM vendors p JOIN users u ON u.id=p.user_id WHERE p.id=NEW.parent_vendor_id AND p.tier=1 AND p.parent_vendor_id IS NULL AND p.status='active' AND u.role='vendor_level1' AND u.status='active' AND u.deleted_at IS NULL FOR SHARE OF p,u;
  IF NOT FOUND THEN RAISE EXCEPTION 'supplier parent must be an active level one supplier'; END IF;
 END IF;
 IF TG_OP='INSERT' THEN
  PERFORM 1 FROM users u WHERE u.id=NEW.user_id AND u.role=CASE NEW.tier WHEN 1 THEN 'vendor_level1' ELSE 'vendor_level2' END AND u.deleted_at IS NULL FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'supplier login role must match tier'; END IF;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS vendors_hierarchy_guard ON vendors;
CREATE TRIGGER vendors_hierarchy_guard BEFORE INSERT OR UPDATE OF tier,parent_vendor_id,user_id ON vendors FOR EACH ROW EXECUTE FUNCTION check_vendor_hierarchy();

-- Role changes through general personnel endpoints cannot turn a supplier into
-- a customer or move its hierarchy. Disabling the login remains supported.
CREATE OR REPLACE FUNCTION protect_vendor_login_role() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.role IS DISTINCT FROM OLD.role AND EXISTS(SELECT 1 FROM vendors WHERE user_id=OLD.id) THEN
  RAISE EXCEPTION 'supplier login role is immutable';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS users_vendor_role_guard ON users;
CREATE TRIGGER users_vendor_role_guard BEFORE UPDATE OF role ON users FOR EACH ROW EXECUTE FUNCTION protect_vendor_login_role();

CREATE OR REPLACE FUNCTION invalidate_vendor_tree() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO scheduler_outbox(event_type,account_id)
 SELECT 'account_changed',a.id FROM accounts a JOIN vendors v ON v.id=a.vendor_id
 WHERE a.deleted_at IS NULL AND (v.user_id=NEW.id OR v.parent_vendor_id IN(SELECT id FROM vendors WHERE user_id=NEW.id));
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS users_vendor_tree_invalidation ON users;
CREATE TRIGGER users_vendor_tree_invalidation AFTER UPDATE OF role,status,deleted_at ON users FOR EACH ROW EXECUTE FUNCTION invalidate_vendor_tree();
