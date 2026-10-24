-- Remove both the standalone provider and the OAuth Excel protocol.
-- Preserve account/group IDs referenced by billing history, OAuth credentials,
-- Codex diagnostic settings/results, and unrelated quality rules. Historical
-- migrations remain immutable; this file is applied transactionally.

DROP TABLE IF EXISTS pg_temp.removed_protocol_accounts;
DROP TABLE IF EXISTS pg_temp.removed_protocol_groups;

CREATE TEMP TABLE removed_protocol_accounts ON COMMIT DROP AS
SELECT a.id, (SELECT jsonb_agg(ag.group_id) FROM account_groups ag WHERE ag.account_id = a.id) AS group_ids
FROM accounts a WHERE a.platform = 'openai_bps';
CREATE TEMP TABLE removed_protocol_groups ON COMMIT DROP AS
SELECT id FROM groups WHERE platform = 'openai_bps';

-- Drop scope uniqueness before removing the BPS action: an account may already
-- have both historical scopes. The retired runner needs no replacement index.
DROP INDEX IF EXISTS scheduled_test_quality_account_scope_unique;
DROP INDEX IF EXISTS scheduled_test_quality_account_unique;

-- Retire old jobs before clearing their routing/configuration. Keep their
-- diagnostic history, and never turn a non-NULL legacy plan into a normal probe.
UPDATE scheduled_test_plans
SET enabled = FALSE, auto_recover = FALSE, next_run_at = NULL,
    running_until = NULL, updated_at = NOW(),
    pelican_config = COALESCE(CASE
      WHEN pelican_config->'quality'->>'action' = 'enable_bps'
        THEN (pelican_config - 'bps') #- '{quality,bps}' #- '{quality,action}'
      ELSE (pelican_config - 'bps') #- '{quality,bps}'
    END, '{}'::jsonb)
WHERE account_id IN (SELECT id FROM removed_protocol_accounts)
   OR pelican_config ? 'bps'
   OR pelican_config->'quality' ? 'bps'
   OR pelican_config->'quality'->>'action' = 'enable_bps';

-- These are live routing/configuration objects, not usage or diagnostic rows.
DELETE FROM composite_model_routes
WHERE target_platform = 'openai_bps'
   OR group_id IN (SELECT id FROM removed_protocol_groups);
DELETE FROM channel_monitors
WHERE provider = 'openai_bps'
   OR account_id IN (SELECT id FROM removed_protocol_accounts);
DELETE FROM channel_monitor_request_templates WHERE provider = 'openai_bps';
DELETE FROM user_platform_quotas WHERE platform = 'openai_bps';
DELETE FROM account_groups
WHERE account_id IN (SELECT id FROM removed_protocol_accounts)
   OR group_id IN (SELECT id FROM removed_protocol_groups);
DELETE FROM model_catalog_sources
WHERE platform = 'openai_bps'
   OR account_id IN (SELECT id FROM removed_protocol_accounts);
DELETE FROM model_catalog_jobs WHERE account_id IN (SELECT id FROM removed_protocol_accounts);
DELETE FROM model_catalog_observations WHERE account_id IN (SELECT id FROM removed_protocol_accounts);

-- Existing protocol-selected OAuth accounts are paused for explicit native
-- revalidation; removing a flag must not silently send their next request to a
-- different upstream. Standalone credentials have no remaining consumer.
WITH changed AS (
    UPDATE accounts a
    SET status = CASE WHEN a.platform = 'openai_bps'
                       OR (a.platform = 'openai' AND a.type = 'oauth' AND a.extra->'openai_excel_bps' = 'true'::jsonb)
                      THEN 'disabled' ELSE a.status END,
        schedulable = CASE WHEN a.platform = 'openai_bps'
                            OR (a.platform = 'openai' AND a.type = 'oauth' AND a.extra->'openai_excel_bps' = 'true'::jsonb)
                           THEN FALSE ELSE a.schedulable END,
        credentials = CASE WHEN a.platform = 'openai_bps' THEN '{}'::jsonb ELSE a.credentials END,
        deleted_at = CASE WHEN a.platform = 'openai_bps' THEN COALESCE(a.deleted_at, NOW()) ELSE a.deleted_at END,
        extra = COALESCE((SELECT jsonb_object_agg(k, v)
                          FROM jsonb_each(CASE WHEN jsonb_typeof(a.extra) = 'object' THEN a.extra ELSE '{}'::jsonb END) e(k, v)
                          WHERE k !~ '^(openai_excel_bps($|_)|openai_bps($|_))'), '{}'::jsonb),
        updated_at = NOW()
    WHERE (a.platform = 'openai_bps' AND (a.deleted_at IS NULL OR a.credentials IS DISTINCT FROM '{}'::jsonb
           OR a.status IS DISTINCT FROM 'disabled' OR a.schedulable IS DISTINCT FROM FALSE))
       OR EXISTS (SELECT 1 FROM jsonb_object_keys(CASE WHEN jsonb_typeof(a.extra) = 'object' THEN a.extra ELSE '{}'::jsonb END) k
                  WHERE k ~ '^(openai_excel_bps($|_)|openai_bps($|_))')
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id, payload)
SELECT 'account_changed', changed.id,
       CASE WHEN old.group_ids IS NOT NULL THEN jsonb_build_object('group_ids', old.group_ids) END
FROM changed LEFT JOIN removed_protocol_accounts old ON old.id = changed.id;

WITH changed AS (
    UPDATE groups SET deleted_at = COALESCE(deleted_at, NOW()), status = 'disabled', updated_at = NOW()
    WHERE id IN (SELECT id FROM removed_protocol_groups)
      AND (deleted_at IS NULL OR status IS DISTINCT FROM 'disabled')
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, group_id)
SELECT 'group_changed', id FROM changed;

DELETE FROM settings
WHERE key ~ '^(excel_bps($|_)|openai_bps($|_)|auto_bps($|_))';

-- Keep every currently allowed provider except the removed one; do not restore
-- an obsolete hard-coded provider list from migration 245.
DO $$
DECLARE item RECORD; definition TEXT;
BEGIN
    FOR item IN
        SELECT c.oid, c.conname, c.conrelid
        FROM pg_constraint c
        WHERE c.contype = 'c'
          AND c.conrelid IN ('user_platform_quotas'::regclass,
              'composite_model_routes'::regclass, 'channel_monitors'::regclass,
              'channel_monitor_request_templates'::regclass)
          AND pg_get_constraintdef(c.oid) LIKE '%openai_bps%'
    LOOP
        definition := replace(pg_get_constraintdef(item.oid), ', ''openai_bps''::text', '');
        definition := replace(definition, '''openai_bps''::text, ', '');
        definition := replace(definition, ', ''openai_bps''::character varying', '');
        definition := replace(definition, '''openai_bps''::character varying, ', '');
        definition := regexp_replace(definition, ',openai_bps([,}])', '\1', 'g');
        definition := replace(definition, '{openai_bps,', '{');
        IF definition LIKE '%openai_bps%' THEN
            RAISE EXCEPTION 'Cannot safely remove retired provider from constraint %: %', item.conname, definition;
        END IF;
        EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', item.conrelid::regclass, item.conname);
        EXECUTE format('ALTER TABLE %s ADD CONSTRAINT %I %s', item.conrelid::regclass, item.conname, definition);
    END LOOP;
END $$;
