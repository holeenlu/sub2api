-- Finish the one-time transfer without replacing an administrator's newer
-- value, changing other scopes, or retaining a runtime compatibility setting.
DO $$
DECLARE backup TEXT; raw_value TEXT; parsed JSONB;
BEGIN
    SELECT value INTO backup FROM settings WHERE key = 'migration_fable_threshold_before_268';
    IF backup IS NULL THEN RETURN; END IF;
    SELECT value INTO raw_value FROM settings WHERE key = 'account_scheduling_thresholds';
    BEGIN parsed := COALESCE(raw_value, '{}')::jsonb;
    EXCEPTION WHEN invalid_text_representation THEN RETURN;
    END;
    IF jsonb_typeof(parsed) <> 'object' THEN RETURN; END IF;
    IF NOT parsed ? 'anthropic_fable' THEN
        INSERT INTO settings (key, value)
        VALUES ('account_scheduling_thresholds',
                (parsed || jsonb_build_object('anthropic_fable', backup::jsonb))::text)
        ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
    END IF;
    DELETE FROM settings WHERE key = 'migration_fable_threshold_before_268';
END $$;
