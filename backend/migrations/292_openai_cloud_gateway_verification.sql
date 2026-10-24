-- The revised ingress/error/transport contract requires fresh model evidence.
-- Do not rewrite the original migration or silently retain old verification.
UPDATE accounts SET extra=jsonb_set(extra,'{openai_cloud}','false'::jsonb,TRUE)
 WHERE extra->'openai_cloud'='true'::jsonb;
UPDATE openai_cloud_grants SET models='{}'::jsonb,revision=revision+1,updated_at=NOW();

CREATE OR REPLACE FUNCTION enforce_openai_cloud_account_settings() RETURNS TRIGGER AS $$
DECLARE
    binding openai_cloud_grants%ROWTYPE;
    model JSONB;
    enabled BOOLEAN;
    models JSONB;
    identity_changed BOOLEAN := FALSE;
BEGIN
    SELECT * INTO binding FROM openai_cloud_grants WHERE account_id = NEW.id;
    IF binding.account_id IS NULL
       AND NOT COALESCE(NEW.extra ? 'openai_cloud', FALSE)
       AND NOT COALESCE(NEW.extra ? 'openai_cloud_models', FALSE) THEN
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
                revoked=TRUE, subject='', models='{}'::jsonb, revision=revision+1,
                workspace_id=COALESCE(NEW.credentials->>'chatgpt_account_id',''),
                user_id=COALESCE(NEW.credentials->>'chatgpt_user_id',''),updated_at=NOW()
                WHERE account_id=NEW.id;
            NEW.extra := jsonb_set(COALESCE(NEW.extra,'{}'::jsonb),'{openai_cloud}','false'::jsonb,TRUE);
        END IF;
    END IF;

    IF TG_OP = 'UPDATE' AND binding.account_id IS NOT NULL AND NOT identity_changed
       AND (OLD.proxy_id IS DISTINCT FROM NEW.proxy_id OR
            OLD.credentials->'model_mapping' IS DISTINCT FROM NEW.credentials->'model_mapping') THEN
        UPDATE openai_cloud_grants SET models='{}'::jsonb, revision=revision+1, updated_at=NOW() WHERE account_id=NEW.id;
        NEW.extra := jsonb_set(COALESCE(NEW.extra,'{}'::jsonb),'{openai_cloud}','false'::jsonb,TRUE);
    END IF;

    IF NEW.extra ? 'openai_cloud' AND jsonb_typeof(NEW.extra->'openai_cloud') IS DISTINCT FROM 'boolean' THEN
        RAISE EXCEPTION 'Invalid Cloud enable setting' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
    END IF;
    enabled := COALESCE(NEW.extra->'openai_cloud' = 'true'::jsonb,FALSE);
    models := COALESCE(NEW.extra->'openai_cloud_models','[]'::jsonb);
    IF jsonb_typeof(models) IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'Invalid Cloud model list' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
    END IF;
    IF jsonb_array_length(models)>64 THEN
        RAISE EXCEPTION 'Cloud model list exceeds limit' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
    END IF;
    FOR model IN SELECT value FROM jsonb_array_elements(models) LOOP
        IF jsonb_typeof(model) IS DISTINCT FROM 'string' OR length(model#>>'{}')=0
           OR length(model#>>'{}')>200 OR btrim(model#>>'{}') IS DISTINCT FROM (model#>>'{}') THEN
            RAISE EXCEPTION 'Invalid Cloud model' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
        END IF;
    END LOOP;
    IF NOT enabled THEN RETURN NEW; END IF;

    IF NEW.platform <> 'openai' OR NEW.type <> 'oauth' OR NEW.vendor_id IS NOT NULL
       OR NEW.parent_account_id IS NOT NULL OR NEW.deleted_at IS NOT NULL
       OR COALESCE(NULLIF(NEW.credentials->>'client_id',''),'app_EMoamEEZ73f0CkXaXp7hrann') <> 'app_EMoamEEZ73f0CkXaXp7hrann'
       OR lower(btrim(COALESCE(NEW.credentials->>'auth_mode',''))) IN ('agentidentity','personalaccesstoken','personal_access_token','pat')
       OR lower(btrim(COALESCE(NEW.credentials->>'openai_auth_mode',''))) IN ('personalaccesstoken','personal_access_token','pat')
       OR NEW.extra->'openai_excel_bps' = 'true'::jsonb
       OR jsonb_array_length(models)=0 OR binding.account_id IS NULL OR binding.revoked
       OR binding.runtime_blocked OR binding.subject='' OR binding.protocol_version <> '2026-10-24.2'
       OR binding.workspace_id IS DISTINCT FROM NEW.credentials->>'chatgpt_account_id'
       OR binding.user_id IS DISTINCT FROM NEW.credentials->>'chatgpt_user_id' THEN
        RAISE EXCEPTION 'Cloud account is not authorized for scheduling' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
    END IF;
    FOR model IN SELECT value FROM jsonb_array_elements(models) LOOP
        IF NOT COALESCE((binding.models->(model#>>'{}')) @> '{"text":true,"tools":true,"usage":true,"settlement":true,"image_input":true,"encrypted_context":true,"web_search":true,"image_generation":true}'::jsonb,FALSE)
           OR length(COALESCE(binding.models->(model#>>'{}')->>'settings_digest',''))<>64
           OR jsonb_typeof(binding.models->(model#>>'{}')->'verified_at') IS DISTINCT FROM 'string'
           OR COALESCE(binding.models->(model#>>'{}')->>'verified_at','') IN ('','0001-01-01T00:00:00Z') THEN
            RAISE EXCEPTION 'Cloud model has not passed complete verification' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER accounts_openai_cloud_settings ON accounts;
CREATE TRIGGER accounts_openai_cloud_settings
    BEFORE INSERT OR UPDATE OF credentials,extra,platform,type,vendor_id,parent_account_id,deleted_at,proxy_id
    ON accounts FOR EACH ROW EXECUTE FUNCTION enforce_openai_cloud_account_settings();

-- Editing the same proxy ID also invalidates the old network-path evidence.
CREATE FUNCTION invalidate_cloud_proxy_verification() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.protocol IS DISTINCT FROM NEW.protocol OR OLD.host IS DISTINCT FROM NEW.host
       OR OLD.port IS DISTINCT FROM NEW.port OR OLD.username IS DISTINCT FROM NEW.username
       OR OLD.password IS DISTINCT FROM NEW.password THEN
        UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{openai_cloud}','false'::jsonb,TRUE),updated_at=NOW()
          WHERE proxy_id=NEW.id AND id IN (SELECT account_id FROM openai_cloud_grants);
        UPDATE openai_cloud_grants SET models='{}'::jsonb,revision=revision+1,updated_at=NOW()
          WHERE account_id IN (SELECT id FROM accounts WHERE proxy_id=NEW.id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER proxies_cloud_verification AFTER UPDATE OF protocol,host,port,username,password ON proxies
    FOR EACH ROW EXECUTE FUNCTION invalidate_cloud_proxy_verification();

-- An operator correction is immutable and auditable; it is never inferred
-- from missing usage or accepted from an ordinary gateway API key.
CREATE TABLE openai_cloud_reconciliations (
    root_id TEXT PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    actor_id BIGINT NOT NULL REFERENCES users(id),
    evidence TEXT NOT NULL CHECK(length(evidence) BETWEEN 5 AND 2000),
    reason TEXT NOT NULL CHECK(length(reason) BETWEEN 5 AND 2000),
    settlement JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE openai_cloud_reconciliations ADD COLUMN settled_at TIMESTAMPTZ;
CREATE TABLE openai_cloud_runtime_blocks (
    root_id TEXT PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX openai_cloud_runtime_blocks_account ON openai_cloud_runtime_blocks(account_id);
CREATE INDEX openai_cloud_reconciliations_pending ON openai_cloud_reconciliations(account_id) WHERE settled_at IS NULL;
