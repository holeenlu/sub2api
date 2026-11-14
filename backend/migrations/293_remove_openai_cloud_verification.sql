-- Cloud authorization does not depend on model verification. Models
-- are admitted by the existing group/account model policy, not a second list.
-- Keep existing grants and selected protocol unchanged; do not repeat the
-- historical verification reset in migration 292. Cloud overrides BPS at
-- dispatch time; both saved switches and the independent Excel grant survive.
DROP TRIGGER IF EXISTS accounts_openai_cloud_settings ON accounts;
DROP TRIGGER IF EXISTS proxies_cloud_verification ON proxies;
DROP FUNCTION IF EXISTS invalidate_cloud_proxy_verification();

CREATE OR REPLACE FUNCTION enforce_openai_cloud_account_settings() RETURNS TRIGGER AS $$
DECLARE
    binding openai_cloud_grants%ROWTYPE;
    enabled BOOLEAN;
    identity_changed BOOLEAN := FALSE;
BEGIN
    IF COALESCE(NEW.extra ? 'openai_cloud_models', FALSE) THEN
        RAISE EXCEPTION 'Cloud model list is retired; use account and group model settings'
            USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
    END IF;
    SELECT * INTO binding FROM openai_cloud_grants WHERE account_id = NEW.id;
    IF binding.account_id IS NULL AND NOT COALESCE(NEW.extra ? 'openai_cloud', FALSE) THEN
        RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' AND binding.account_id IS NOT NULL THEN
        identity_changed := OLD.platform IS DISTINCT FROM NEW.platform
            OR OLD.type IS DISTINCT FROM NEW.type
            OR OLD.vendor_id IS DISTINCT FROM NEW.vendor_id
            OR OLD.parent_account_id IS DISTINCT FROM NEW.parent_account_id
            OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
            OR OLD.credentials->>'chatgpt_account_id' IS DISTINCT FROM NEW.credentials->>'chatgpt_account_id'
            OR OLD.credentials->>'chatgpt_user_id' IS DISTINCT FROM NEW.credentials->>'chatgpt_user_id'
            OR COALESCE(NULLIF(OLD.credentials->>'client_id',''),'app_EMoamEEZ73f0CkXaXp7hrann')
                IS DISTINCT FROM COALESCE(NULLIF(NEW.credentials->>'client_id',''),'app_EMoamEEZ73f0CkXaXp7hrann')
            OR OLD.credentials->>'auth_mode' IS DISTINCT FROM NEW.credentials->>'auth_mode'
            OR OLD.credentials->>'openai_auth_mode' IS DISTINCT FROM NEW.credentials->>'openai_auth_mode';
        IF identity_changed THEN
            UPDATE openai_cloud_grants SET generation=gen_random_uuid()::text,
                revoked=TRUE, subject='', revision=revision+1,
                workspace_id=COALESCE(NEW.credentials->>'chatgpt_account_id',''),
                user_id=COALESCE(NEW.credentials->>'chatgpt_user_id',''),updated_at=NOW()
                WHERE account_id=NEW.id;
            NEW.extra := jsonb_set(COALESCE(NEW.extra,'{}'::jsonb),'{openai_cloud}','false'::jsonb,TRUE);
        END IF;
    END IF;

    IF NEW.extra ? 'openai_cloud' AND jsonb_typeof(NEW.extra->'openai_cloud') IS DISTINCT FROM 'boolean' THEN
        RAISE EXCEPTION 'Invalid Cloud enable setting' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
    END IF;
    enabled := COALESCE(NEW.extra->'openai_cloud' = 'true'::jsonb,FALSE);
    IF NOT enabled THEN RETURN NEW; END IF;

    IF NEW.platform <> 'openai' OR NEW.type <> 'oauth' OR NEW.vendor_id IS NOT NULL
       OR NEW.parent_account_id IS NOT NULL OR NEW.deleted_at IS NOT NULL
       OR COALESCE(NULLIF(NEW.credentials->>'client_id',''),'app_EMoamEEZ73f0CkXaXp7hrann') <> 'app_EMoamEEZ73f0CkXaXp7hrann'
       OR lower(btrim(COALESCE(NEW.credentials->>'auth_mode',''))) IN ('agentidentity','personalaccesstoken','personal_access_token','pat')
       OR lower(btrim(COALESCE(NEW.credentials->>'openai_auth_mode',''))) IN ('personalaccesstoken','personal_access_token','pat')
       OR binding.account_id IS NULL OR binding.revoked OR binding.runtime_blocked OR binding.subject=''
       OR binding.workspace_id IS DISTINCT FROM NEW.credentials->>'chatgpt_account_id'
       OR binding.user_id IS DISTINCT FROM NEW.credentials->>'chatgpt_user_id'
       OR EXISTS(SELECT 1 FROM openai_cloud_runtime_blocks b
                 LEFT JOIN openai_cloud_reconciliations r USING(root_id)
                 WHERE b.account_id=NEW.id AND r.settled_at IS NULL) THEN
        RAISE EXCEPTION 'Cloud account is not authorized for scheduling' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Remove only the retired feature's data. This transaction holds the table DDL
-- lock while its old admission trigger is absent, so enabled accounts do not
-- need a temporary invalid model list or a protocol toggle during cleanup.
UPDATE accounts SET extra=extra-'openai_cloud_models'
 WHERE extra ? 'openai_cloud_models';
ALTER TABLE openai_cloud_grants DROP COLUMN models, DROP COLUMN protocol_version;
CREATE TRIGGER accounts_openai_cloud_settings
    BEFORE INSERT OR UPDATE OF credentials,extra,platform,type,vendor_id,parent_account_id,deleted_at,proxy_id
    ON accounts FOR EACH ROW EXECUTE FUNCTION enforce_openai_cloud_account_settings();
