-- Nullable on purpose: previous executions cannot be classified from today's
-- credentials, switches or group names. Do not backfill historical usage.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS authorization_mode VARCHAR(6)
 CHECK (authorization_mode IN ('native', 'bps'));

CREATE OR REPLACE FUNCTION guard_usage_authorization_mode() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.authorization_mode IS DISTINCT FROM OLD.authorization_mode THEN
  RAISE EXCEPTION 'usage authorization mode is immutable';
 END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS usage_logs_authorization_mode ON usage_logs;
CREATE TRIGGER usage_logs_authorization_mode BEFORE UPDATE OF authorization_mode
 ON usage_logs FOR EACH ROW EXECUTE FUNCTION guard_usage_authorization_mode();
