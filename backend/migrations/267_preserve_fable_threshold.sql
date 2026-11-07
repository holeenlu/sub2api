-- Fable is retained again. Preserve its configured value across the immutable
-- 268 retirement migration, whose transaction otherwise removes that scope.
-- Already-upgraded databases have no recoverable value; do not guess one.
DO $$
DECLARE raw_value TEXT; parsed JSONB;
BEGIN
    IF EXISTS (SELECT 1 FROM schema_migrations WHERE filename = '268_retire_fork_gateway_runtime.sql') THEN
        RETURN;
    END IF;
    SELECT value INTO raw_value FROM settings WHERE key = 'account_scheduling_thresholds';
    IF raw_value IS NULL THEN RETURN; END IF;
    BEGIN parsed := raw_value::jsonb;
    EXCEPTION WHEN invalid_text_representation THEN RETURN;
    END;
    IF jsonb_typeof(parsed) = 'object' AND parsed ? 'anthropic_fable' THEN
        INSERT INTO settings (key, value)
        VALUES ('migration_fable_threshold_before_268', (parsed->'anthropic_fable')::text)
        ON CONFLICT (key) DO NOTHING;
    END IF;
END $$;
