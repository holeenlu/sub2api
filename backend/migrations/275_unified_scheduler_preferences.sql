-- Snapshot the old database inputs before the startup migration. Completion
-- happens in migrateSchedulerBusinessPreferences, under this row's lock, so
-- effective YAML/env fallback cost intent can be included in the same decision.
-- Retired keys are retained solely for audit/rollback, never read by ranking.
INSERT INTO settings (key,value)
SELECT 'migration_275_scheduler_preferences_backup', COALESCE(jsonb_object_agg(key,value),'{}'::jsonb)::text
FROM settings WHERE key LIKE 'openai_advanced_scheduler_%' OR key IN
 ('openai_low_upstream_rate_priority_enabled','openai_oauth_scheduling_rate_multiplier')
ON CONFLICT (key) DO NOTHING;
