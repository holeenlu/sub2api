-- Retire the removed runtime settings without changing native account access,
-- credentials, billing history, or the retained diagnostic plans/results.
-- Transfer the retained template once; an existing diagnostic value (including
-- an explicit empty/default value) takes precedence. Runtime reads only the new key.
INSERT INTO settings (key, value)
SELECT 'openai_codex_diagnostic_prompt_template', value
FROM settings WHERE key = 'openai_codex_ticket_prompt_template'
ON CONFLICT (key) DO NOTHING;

DELETE FROM settings
WHERE key ~ '^openai_codex_ticket_'
   OR key IN ('model_catalog_settings', 'model_catalog_registry', 'upstream_failover_status_codes');

-- Native thresholds have platform scopes only. Keep every native setting and
-- leave unrelated malformed historical values for the normal settings validator.
DO $$
DECLARE raw_value TEXT; parsed JSONB;
BEGIN
    SELECT value INTO raw_value FROM settings WHERE key = 'account_scheduling_thresholds';
    IF raw_value IS NULL THEN RETURN; END IF;
    BEGIN parsed := raw_value::jsonb;
    EXCEPTION WHEN invalid_text_representation THEN RETURN;
    END;
    IF jsonb_typeof(parsed) = 'object' AND parsed ? 'anthropic_fable' THEN
        UPDATE settings SET value = (parsed - 'anthropic_fable')::text
        WHERE key = 'account_scheduling_thresholds';
    END IF;
END $$;

WITH changed AS (
    UPDATE accounts a
    SET extra = COALESCE((
          SELECT jsonb_object_agg(k, v)
          FROM jsonb_each(CASE WHEN jsonb_typeof(a.extra) = 'object' THEN a.extra ELSE '{}'::jsonb END) e(k, v)
          WHERE k !~ '^(codex_turn_ticket:|codex_ticket_|openai_codex_ticket_)'
            AND k NOT IN ('codex_allow_without_ticket', 'codex_harvest_proxy_url',
                'openai_apikey_codex_identity', 'openai_oauth_ws_sse_acceleration',
                'model_catalog_snapshot', 'model_catalog_policy')
        ), '{}'::jsonb),
        updated_at = NOW()
    WHERE EXISTS (
        SELECT 1 FROM jsonb_object_keys(CASE WHEN jsonb_typeof(a.extra) = 'object' THEN a.extra ELSE '{}'::jsonb END) k
        WHERE k ~ '^(codex_turn_ticket:|codex_ticket_|openai_codex_ticket_)'
           OR k IN ('codex_allow_without_ticket', 'codex_harvest_proxy_url',
               'openai_apikey_codex_identity', 'openai_oauth_ws_sse_acceleration',
               'model_catalog_snapshot', 'model_catalog_policy')
    )
    RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM changed;

-- Preserve audit metadata, including past diagnostic observations, but discard
-- credentials that belonged only to the removed ticket binding mechanism.
UPDATE codex_ticket_invalidations
SET original_ticket = NULL, original_cookie = NULL, returned_ticket = '',
    returned_cookie = NULL, returned_set_cookies = NULL
WHERE original_ticket IS NOT NULL OR original_cookie IS NOT NULL
   OR returned_ticket <> '' OR returned_cookie IS NOT NULL
   OR returned_set_cookies IS NOT NULL;
