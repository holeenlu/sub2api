-- 264 was published: preserve its checksum, migrate once, then remove obsolete
-- tables. Runtime code uses only the existing scheduled-test plan/result store.
ALTER TABLE scheduled_test_plans ADD COLUMN diagnostic_config JSONB;
ALTER TABLE scheduled_test_results ADD COLUMN diagnostic_run JSONB;
ALTER TABLE scheduled_test_results ALTER COLUMN started_at DROP NOT NULL;
ALTER TABLE scheduled_test_results ALTER COLUMN finished_at DROP NOT NULL;
CREATE UNIQUE INDEX scheduled_test_diagnostic_account ON scheduled_test_plans(account_id)
 WHERE diagnostic_config IS NOT NULL;
CREATE UNIQUE INDEX scheduled_test_diagnostic_active ON scheduled_test_results(plan_id)
 WHERE diagnostic_run IS NOT NULL AND status IN ('queued','running');
CREATE INDEX scheduled_test_diagnostic_queue ON scheduled_test_results(id)
 WHERE diagnostic_run IS NOT NULL AND status='queued';

INSERT INTO scheduled_test_plans(account_id,enabled,max_results,next_run_at,updated_at,diagnostic_config)
 SELECT account_id,enabled,10,next_run_at,updated_at,
 jsonb_build_object('owner_id',owner_id,'api_key_id',COALESCE(api_key_id,0),'models',models,
 'interval_minutes',interval_minutes,'revision',revision) FROM codex_diagnostic_plans;

INSERT INTO scheduled_test_results(plan_id,status,error_message,started_at,finished_at,created_at,diagnostic_run)
 SELECT p.id,
 CASE WHEN r.status IN ('queued','running') THEN 'failed' ELSE r.status END,
 CASE WHEN r.status IN ('queued','running') THEN 'worker_interrupted' ELSE r.reason END,
 r.started_at,CASE WHEN r.status IN ('queued','running') THEN NOW() ELSE r.finished_at END,r.created_at,
 jsonb_build_object('owner_id',r.owner_id,'api_key_id',COALESCE(r.api_key_id,0),'api_key_name',r.api_key_name,
 'plan_revision',r.plan_revision,'models',r.models,'source',r.source,'items',r.items,'cancel_requested',r.cancel_requested)
 FROM codex_diagnostic_runs r JOIN scheduled_test_plans p ON p.account_id=r.account_id AND p.diagnostic_config IS NOT NULL
 ORDER BY r.id;

DELETE FROM scheduled_test_results WHERE id IN (
 SELECT id FROM (SELECT id,ROW_NUMBER() OVER (PARTITION BY plan_id ORDER BY id DESC) AS n
 FROM scheduled_test_results WHERE diagnostic_run IS NOT NULL) ranked WHERE n>10);

DROP TABLE codex_diagnostic_runs;
DROP TABLE codex_diagnostic_plans;
