-- Cloud uses native Codex OAuth directly. The switch does not require a separate
-- login/binding; verified execution bindings are created on first use. Keep all
-- identity-change and durable unresolved-consumption protections.
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
       OR COALESCE(NEW.credentials->>'chatgpt_account_id','')=''
       OR COALESCE(NEW.credentials->>'chatgpt_user_id','')=''
       OR COALESCE(binding.runtime_blocked,FALSE)
       OR EXISTS(SELECT 1 FROM openai_cloud_runtime_blocks b
                 LEFT JOIN openai_cloud_reconciliations r USING(root_id)
                 WHERE b.account_id=NEW.id AND r.settled_at IS NULL) THEN
        RAISE EXCEPTION 'Cloud account is not authorized for scheduling' USING ERRCODE='23514', CONSTRAINT='accounts_openai_cloud_settings';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
