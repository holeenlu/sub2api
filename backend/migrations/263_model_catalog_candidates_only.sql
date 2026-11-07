-- Restore credentials.model_mapping as the account whitelist/routing owner.
-- This DO block is atomic: ambiguous policies abort the entire migration and
-- report account IDs for explicit administrator resolution before upgrading.
DO $$
DECLARE
    account_row RECORD;
    policy JSONB;
    mapping JSONB;
    converted JSONB;
    selected JSONB;
    model TEXT;
    passthrough BOOLEAN;
    aliases BOOLEAN;
    invalid_ids BIGINT[] := '{}';
BEGIN
    FOR account_row IN
        SELECT id, platform, type, parent_account_id, credentials, extra
        FROM accounts WHERE extra ? 'model_catalog_policy' FOR UPDATE
    LOOP
        policy := account_row.extra->'model_catalog_policy';
        mapping := COALESCE(NULLIF(account_row.credentials->'model_mapping', 'null'::jsonb), '{}'::jsonb);
        converted := mapping;
        IF policy <> 'null'::jsonb AND COALESCE(policy->>'mode', '') <> 'legacy' THEN
            IF jsonb_typeof(policy) <> 'object'
               OR (policy ? 'models' AND jsonb_typeof(policy->'models') NOT IN ('array', 'null'))
               OR jsonb_typeof(mapping) <> 'object' THEN
                invalid_ids := array_append(invalid_ids, account_row.id);
                CONTINUE;
            END IF;
            selected := COALESCE(NULLIF(policy->'models', 'null'::jsonb), '[]'::jsonb);
            IF EXISTS (SELECT 1 FROM jsonb_array_elements(selected) AS item
                       WHERE jsonb_typeof(item) <> 'string' OR btrim(item #>> '{}') = ''
                          OR (item #>> '{}') ~ '[*\n\r]')
               OR EXISTS (SELECT 1 FROM jsonb_each(mapping) AS entry
                          WHERE jsonb_typeof(entry.value) <> 'string'
                             OR entry.key LIKE '%*%' OR entry.value #>> '{}' LIKE '%*%') THEN
                invalid_ids := array_append(invalid_ids, account_row.id);
                CONTINUE;
            END IF;
            passthrough := account_row.platform = 'openai' AND
                (account_row.extra->'openai_passthrough' = 'true'::jsonb
                 OR account_row.extra->'openai_oauth_passthrough' = 'true'::jsonb);
            aliases := account_row.platform = 'openai' AND account_row.type = 'oauth'
                AND account_row.parent_account_id IS NULL
                AND account_row.credentials->>'model_mapping_mode' = 'aliases';
            IF jsonb_array_length(selected) = 0 THEN
                -- Identity-only old whitelists may be dropped for unrestricted accounts.
                IF NOT COALESCE(passthrough OR aliases, FALSE) THEN
                    IF EXISTS (SELECT 1 FROM jsonb_each_text(mapping) WHERE key <> value) THEN
                        invalid_ids := array_append(invalid_ids, account_row.id);
                        CONTINUE;
                    END IF;
                    converted := '{}'::jsonb;
                END IF;
            ELSE
                -- Native passthrough and platform-injected aliases cannot encode
                -- this separate restriction without changing the routing contract.
                IF COALESCE(passthrough, FALSE) OR account_row.platform = 'antigravity'
                   OR (mapping = '{}'::jsonb AND account_row.platform IN ('grok', 'gemini')) THEN
                    invalid_ids := array_append(invalid_ids, account_row.id);
                    CONTINUE;
                END IF;
                converted := '{}'::jsonb;
                FOR model IN SELECT jsonb_array_elements_text(selected) LOOP
                    converted := converted || jsonb_build_object(model, COALESCE(mapping->>model, model));
                END LOOP;
                -- Keep only aliases authorized by the old selection (name or target).
                SELECT converted || COALESCE(jsonb_object_agg(key, value), '{}'::jsonb)
                INTO converted FROM jsonb_each_text(mapping) AS entry
                WHERE EXISTS (SELECT 1 FROM jsonb_array_elements_text(selected) AS allowed(id)
                              WHERE lower(allowed.id) IN (lower(entry.key), lower(entry.value)));
            END IF;
        END IF;
        UPDATE accounts
        SET credentials = CASE
                WHEN policy = 'null'::jsonb OR policy->>'mode' = 'legacy' THEN credentials
                ELSE jsonb_set(
                    CASE WHEN jsonb_array_length(selected) > 0
                         THEN COALESCE(credentials, '{}'::jsonb) - 'model_mapping_mode'
                         ELSE COALESCE(credentials, '{}'::jsonb) END,
                    '{model_mapping}', converted)
                END,
            extra = extra - 'model_catalog_policy' - 'model_catalog_visibility',
            updated_at = NOW()
        WHERE id = account_row.id;
        INSERT INTO scheduler_outbox (event_type, account_id) VALUES ('account_changed', account_row.id);
    END LOOP;
    IF cardinality(invalid_ids) > 0 THEN
        RAISE EXCEPTION 'Cannot losslessly migrate model restrictions for accounts %. Resolve wildcard, passthrough, platform-default or unrestricted alias policies before upgrading; no account changes were saved.', invalid_ids;
    END IF;
END $$;
