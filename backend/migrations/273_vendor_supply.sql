-- Supply identity is additive. Existing users, accounts and proxies remain platform-owned.
CREATE TABLE IF NOT EXISTS vendors (
 id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL UNIQUE REFERENCES users(id),
 name VARCHAR(100) NOT NULL, status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
 policy_version BIGINT NOT NULL DEFAULT 1, platforms JSONB NOT NULL DEFAULT '["anthropic","openai"]',
 account_limit INTEGER NOT NULL DEFAULT 100 CHECK(account_limit BETWEEN 1 AND 10000),
 proxy_limit INTEGER NOT NULL DEFAULT 100 CHECK(proxy_limit BETWEEN 1 AND 10000),
 allow_direct BOOLEAN NOT NULL DEFAULT FALSE,
 default_concurrency INTEGER NOT NULL DEFAULT 3 CHECK(default_concurrency BETWEEN 1 AND 1000),
 default_priority INTEGER NOT NULL DEFAULT 50 CHECK(default_priority BETWEEN 0 AND 10000),
 default_rate_multiplier NUMERIC(20,8) NOT NULL DEFAULT 1 CHECK(default_rate_multiplier >= 0),
 contact_name VARCHAR(100) NOT NULL DEFAULT '', settlement_email VARCHAR(254) NOT NULL DEFAULT '',
 binance_id VARCHAR(100) NOT NULL DEFAULT '', default_coefficient NUMERIC(20,8) NOT NULL DEFAULT 1 CHECK(default_coefficient > 0),
 group_coefficients JSONB NOT NULL DEFAULT '{}', contract_version BIGINT NOT NULL DEFAULT 1,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS vendor_groups (
 id BIGSERIAL PRIMARY KEY, vendor_id BIGINT NOT NULL REFERENCES vendors(id), group_id BIGINT NOT NULL REFERENCES groups(id),
 is_default BOOLEAN NOT NULL DEFAULT FALSE, UNIQUE(vendor_id,group_id)
);
CREATE TABLE IF NOT EXISTS account_supply_identity_claims (
 id BIGSERIAL PRIMARY KEY, platform VARCHAR(50) NOT NULL, identity_hash VARCHAR(128) NOT NULL,
 canonical_account_id BIGINT NOT NULL REFERENCES accounts(id), vendor_id BIGINT REFERENCES vendors(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(platform,identity_hash)
);
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS vendor_id BIGINT REFERENCES vendors(id);
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS vendor_paused_at TIMESTAMPTZ;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS vendor_notes TEXT NOT NULL DEFAULT '';
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS vendor_id BIGINT REFERENCES vendors(id);
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS created_by BIGINT REFERENCES users(id);
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS vendor_notes TEXT NOT NULL DEFAULT '';
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS region_label_country_code VARCHAR(8) NOT NULL DEFAULT '';
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS region_label_city VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS config_version BIGINT NOT NULL DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_accounts_vendor_active ON accounts(vendor_id,deleted_at,status,id) WHERE vendor_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_proxies_vendor_active ON proxies(vendor_id,status,id) WHERE vendor_id IS NOT NULL;
INSERT INTO settings(key,value,updated_at) VALUES ('vendor_console_enabled','false',NOW()),('vendor_portal_origin','',NOW()) ON CONFLICT(key) DO NOTHING;
-- Native binding/fallback paths cannot cross the immutable supply owner.
CREATE OR REPLACE FUNCTION check_supply_proxy_owner() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner BIGINT;
BEGIN
 IF TG_TABLE_NAME='accounts' THEN
  IF TG_OP='UPDATE' AND NEW.vendor_id IS DISTINCT FROM OLD.vendor_id THEN RAISE EXCEPTION 'supply ownership is immutable'; END IF;
  IF NEW.proxy_id IS NOT NULL THEN
   SELECT vendor_id INTO owner FROM proxies WHERE id=NEW.proxy_id FOR KEY SHARE;
   IF owner IS DISTINCT FROM NEW.vendor_id THEN RAISE EXCEPTION 'proxy supply ownership mismatch'; END IF;
  END IF;
  IF NEW.parent_account_id IS NOT NULL THEN
   SELECT vendor_id INTO owner FROM accounts WHERE id=NEW.parent_account_id FOR KEY SHARE;
   IF owner IS DISTINCT FROM NEW.vendor_id THEN RAISE EXCEPTION 'shadow supply ownership mismatch'; END IF;
  END IF;
 ELSE
  IF TG_OP='UPDATE' AND NEW.vendor_id IS DISTINCT FROM OLD.vendor_id THEN RAISE EXCEPTION 'supply ownership is immutable'; END IF;
  IF NEW.backup_proxy_id IS NOT NULL THEN
   SELECT vendor_id INTO owner FROM proxies WHERE id=NEW.backup_proxy_id FOR KEY SHARE;
   IF owner IS DISTINCT FROM NEW.vendor_id THEN RAISE EXCEPTION 'backup supply ownership mismatch'; END IF;
  END IF;
  IF NEW.vendor_id IS NOT NULL AND NEW.fallback_mode='direct' THEN RAISE EXCEPTION 'supplier proxy cannot fall back to direct'; END IF;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS accounts_supply_owner ON accounts;
CREATE TRIGGER accounts_supply_owner BEFORE INSERT OR UPDATE OF vendor_id,proxy_id,parent_account_id ON accounts FOR EACH ROW EXECUTE FUNCTION check_supply_proxy_owner();
DROP TRIGGER IF EXISTS proxies_supply_owner ON proxies;
CREATE TRIGGER proxies_supply_owner BEFORE INSERT OR UPDATE OF vendor_id,backup_proxy_id,fallback_mode ON proxies FOR EACH ROW EXECUTE FUNCTION check_supply_proxy_owner();
