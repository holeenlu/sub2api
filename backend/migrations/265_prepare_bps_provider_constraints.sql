-- Run before 265_remove_bps_protocols.sql without changing that published file
-- or its checksum. PostgreSQL renders varchar IN lists with a nested cast that
-- the original migration cannot remove. Normalize only the retired literal's
-- equivalent text cast; preserve every allowed provider and other predicate.
DO $$
DECLARE item RECORD; definition TEXT; normalized TEXT;
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
        definition := pg_get_constraintdef(item.oid);
        normalized := replace(definition,
            '(''openai_bps''::character varying)::text', '''openai_bps''::text');
        IF normalized IS DISTINCT FROM definition THEN
            EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', item.conrelid::regclass, item.conname);
            EXECUTE format('ALTER TABLE %s ADD CONSTRAINT %I %s', item.conrelid::regclass, item.conname, normalized);
        END IF;
    END LOOP;
END $$;
