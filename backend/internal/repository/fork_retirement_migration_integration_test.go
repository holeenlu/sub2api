//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration253RetiresForkFeaturesWithoutDeletingHistory(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	// Clone the migrated schema, including defaults and checks. Temporary tables
	// isolate the upgrade fixture from other integration tests and real triggers.
	for _, table := range []string{"accounts", "groups", "users", "channel_monitors", "channel_monitor_request_templates", "user_platform_quotas", "composite_model_routes", "scheduled_test_plans", "announcements", "scheduler_outbox", "settings"} {
		_, err := tx.ExecContext(ctx, fmt.Sprintf("CREATE TEMP TABLE %s (LIKE public.%s INCLUDING ALL) ON COMMIT DROP", table, table))
		require.NoError(t, err)
	}
	// Recreate the pre-retirement provider constraints in the temporary schema.
	// The current schema has already removed the old provider in migration 265.
	legacyPlatforms, err := dbmigrations.FS.ReadFile("245_openai_bps_platform.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(legacyPlatforms))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
INSERT INTO accounts (id, name, platform, type, extra, status, schedulable, error_message) VALUES
 (1, 'ticket account', 'openai', 'oauth', '{"codex_turn_ticket:gpt-6-astra":{"state":"keep-ticket"},"codex_ticket_harvest_enabled":true}', 'active', true, NULL),
 (2, 'dedicated BPS', 'openai_bps', 'oauth', '{"history":"keep"}', 'active', true, 'keep diagnostic'),
 (3, 'Copilot', 'openai', 'apikey', '{"openai_copilot_sdk":true}', 'active', true, NULL),
 (4, 'Excel', 'openai', 'oauth', '{"openai_excel_bps":true,"openai_excel_bps_models":["gpt-6-astra"]}', 'active', true, NULL),
 (5, 'already ordinary', 'openai', 'oauth', '{"openai_excel_bps":false}', 'active', true, NULL),
 (6, 'other platform', 'anthropic', 'oauth', '{"openai_excel_bps":true,"openai_copilot_sdk":true}', 'active', true, NULL),
 (7, 'manual error', 'openai', 'oauth', '{}', 'error', false, 'administrator investigation'),
 (8, 'already disabled BPS', 'openai_bps', 'oauth', '{}', 'disabled', false, NULL);
INSERT INTO groups (id, name, platform) VALUES (1, 'ordinary', 'openai'), (2, 'retired', 'openai_bps');
INSERT INTO users (id, email, password_hash, role, balance, observer_group_ids) VALUES
 (1, 'admin@test.invalid', 'fake', 'admin', 11, '[]'),
 (2, 'observer@test.invalid', 'fake', 'observer', 23, '[1,2]'),
 (3, 'user@test.invalid', 'fake', 'user', 7, '[]');
INSERT INTO channel_monitors (id, name, provider, account_id, endpoint, api_key_encrypted, primary_model, interval_seconds, created_by) VALUES
 (1, 'ordinary', 'openai', 1, 'https://example.invalid', 'fake', 'gpt-test', 60, 1),
 (2, 'retired', 'openai_bps', 2, 'https://example.invalid', 'fake', 'gpt-test', 60, 1),
 (3, 'retired account monitor', 'openai', 3, 'https://example.invalid', 'fake', 'gpt-test', 60, 1);
INSERT INTO composite_model_routes (id, group_id, public_model, target_platform) VALUES
 (1, 1, 'ordinary-model', 'openai'), (2, 1, 'retired-model', 'openai_bps');
INSERT INTO scheduled_test_plans (id, account_id, enabled, auto_recover, next_run_at, running_until, pelican_config) VALUES
 (1, 1, true, true, NOW(), NULL, NULL),
 (2, 1, true, true, NOW(), NOW() + INTERVAL '1 hour', '{"prompt":"pelican"}'),
 (3, 1, false, true, NOW(), NULL, '{"quality":{"expected_answer":"keep-history"}}'),
 (4, 3, true, true, NOW(), NULL, NULL);
INSERT INTO announcements (id, title, content, status, targeting) VALUES
 (1, 'ordinary', 'keep public', 'active', '{}'),
 (2, 'targeted', 'keep private', 'active', '{"any_of":[{"all_of":[{"type":"user","operator":"in","user_ids":[2]}]}]}'),
 (3, 'mixed targeting', 'keep mixed', 'draft', '{"any_of":[{"all_of":[{"type":"balance","operator":"gt","value":0}]},{"all_of":[{"type":"user","operator":"in","user_ids":[2]}]}]}');
INSERT INTO settings (key, value) VALUES ('openai_codex_ticket_enabled', 'true'), ('openai_codex_ticket_prompt_template', 'keep custom template');`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("253_retire_fork_features.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		for id, want := range map[int]string{1: "active", 2: "disabled", 3: "disabled", 4: "disabled", 5: "active", 6: "active", 7: "error", 8: "disabled"} {
			var status string
			var schedulable bool
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT status, schedulable FROM accounts WHERE id=$1`, id).Scan(&status, &schedulable))
			require.Equal(t, want, status, "account %d", id)
			require.Equal(t, want == "active", schedulable, "account %d", id)
		}
		var count int
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&count))
		require.Equal(t, 8, count)
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox WHERE event_type='account_changed'`).Scan(&count))
		require.Equal(t, 3, count, "second execution must not duplicate account events")
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox WHERE event_type='group_changed' AND group_id=2`).Scan(&count))
		require.Equal(t, 1, count)
		for _, query := range []string{
			`SELECT COUNT(*) FROM groups WHERE (id=1 AND status='active') OR (id=2 AND status='disabled')`,
			`SELECT COUNT(*) FROM composite_model_routes WHERE (id=1 AND enabled) OR (id=2 AND NOT enabled)`,
		} {
			require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&count))
			require.Equal(t, 2, count)
		}
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_monitors WHERE (id=1 AND enabled) OR (id IN (2,3) AND NOT enabled)`).Scan(&count))
		require.Equal(t, 3, count)
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_test_plans WHERE (id=1 AND enabled AND auto_recover AND next_run_at IS NOT NULL) OR (id IN (2,3,4) AND NOT enabled AND NOT auto_recover AND next_run_at IS NULL AND running_until IS NULL AND (id=4 OR pelican_config IS NOT NULL))`).Scan(&count))
		require.Equal(t, 4, count)
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE (id=1 AND role='admin' AND balance=11) OR (id=2 AND role='user' AND balance=23 AND observer_group_ids='[1,2]'::jsonb) OR (id=3 AND role='user' AND balance=7)`).Scan(&count))
		require.Equal(t, 3, count)
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM announcements WHERE (id=1 AND status='active') OR (id IN (2,3) AND status='archived' AND jsonb_path_exists(targeting, '$.any_of[*].all_of[*] ? (@.type == "user")'))`).Scan(&count))
		require.Equal(t, 3, count)
	}
	var value string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra::text FROM accounts WHERE id=1`).Scan(&value))
	require.JSONEq(t, `{"codex_turn_ticket:gpt-6-astra":{"state":"keep-ticket"},"codex_ticket_harvest_enabled":true}`, value)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra::text FROM accounts WHERE id=4`).Scan(&value))
	require.JSONEq(t, `{"openai_excel_bps":true,"openai_excel_bps_models":["gpt-6-astra"]}`, value)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT error_message FROM accounts WHERE id=2`).Scan(&value))
	require.Equal(t, "keep diagnostic", value)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='openai_codex_ticket_enabled'`).Scan(&value))
	require.Equal(t, "true", value)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='openai_codex_ticket_prompt_template'`).Scan(&value))
	require.Equal(t, "keep custom template", value)
}

func TestRetiredScheduledPlansCannotBecomeConnectivityProbes(t *testing.T) {
	ctx := context.Background()
	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name, platform, type) VALUES ('retired-plan-repository', 'openai', 'oauth') RETURNING id`).Scan(&accountID))
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, accountID) })
	repo := NewScheduledTestPlanRepository(integrationDB)
	due := time.Now().Add(-time.Hour)
	ordinary, err := repo.Create(ctx, &service.ScheduledTestPlan{AccountID: accountID, ModelID: "gpt-test", CronExpression: "*/30 * * * *", Enabled: true, MaxResults: 50, NextRunAt: &due})
	require.NoError(t, err)
	var retiredID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO scheduled_test_plans(account_id, model_id, enabled, next_run_at, pelican_config) VALUES ($1, 'retired', true, $2, '{"quality":{}}') RETURNING id`, accountID, due).Scan(&retiredID))
	_, err = repo.GetByID(ctx, retiredID)
	require.ErrorIs(t, err, sql.ErrNoRows)
	plans, err := repo.ListByAccountID(ctx, accountID)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, ordinary.ID, plans[0].ID)
	plans, err = repo.ListDue(ctx, time.Now())
	require.NoError(t, err)
	var foundOrdinary bool
	for _, plan := range plans {
		require.NotEqual(t, retiredID, plan.ID)
		foundOrdinary = foundOrdinary || plan.ID == ordinary.ID
	}
	require.True(t, foundOrdinary)
	_, err = repo.Update(ctx, &service.ScheduledTestPlan{ID: retiredID, ModelID: "rewritten", Enabled: true})
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.NoError(t, repo.UpdateAfterRun(ctx, retiredID, time.Now(), time.Now().Add(time.Hour)))
	require.NoError(t, repo.Delete(ctx, retiredID))
	var model, config string
	var lastRun sql.NullTime
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT model_id, pelican_config::text, last_run_at FROM scheduled_test_plans WHERE id=$1`, retiredID).Scan(&model, &config, &lastRun))
	require.Equal(t, "retired", model)
	require.JSONEq(t, `{"quality":{}}`, config)
	require.False(t, lastRun.Valid)
	ordinary.ModelID = "updated-model"
	_, err = repo.Update(ctx, ordinary)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, ordinary.ID))
	_, err = repo.GetByID(ctx, ordinary.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestMigration253LedgerDoesNotDisableManuallyRestoredAccounts(t *testing.T) {
	ctx := context.Background()
	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name, platform, type, extra) VALUES ('after-retirement-review', 'openai', 'oauth', '{"openai_excel_bps":true}') RETURNING id`).Scan(&accountID))
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, accountID) })
	// TestMain applied every migration before this administrator-reviewed account
	// was restored; a regular restart must not run the retirement SQL again.
	require.NoError(t, ApplyMigrations(ctx, integrationDB))
	var status string
	var schedulable bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status, schedulable FROM accounts WHERE id=$1`, accountID).Scan(&status, &schedulable))
	require.Equal(t, "active", status)
	require.True(t, schedulable)
}

func TestRemoveProtocolsMigrationPreservesDiagnosticsAndBillingHistory(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	// A pre-upgrade schema, isolated from the already-migrated shared fixture.
	_, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE accounts (id BIGINT PRIMARY KEY, platform TEXT, type TEXT, status TEXT, schedulable BOOLEAN,
 credentials JSONB, extra JSONB, updated_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ) ON COMMIT DROP;
CREATE TEMP TABLE groups (id BIGINT PRIMARY KEY, platform TEXT, status TEXT, updated_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ) ON COMMIT DROP;
CREATE TEMP TABLE account_groups (account_id BIGINT, group_id BIGINT) ON COMMIT DROP;
CREATE TEMP TABLE settings (key TEXT PRIMARY KEY, value TEXT) ON COMMIT DROP;
CREATE TEMP TABLE scheduler_outbox (event_type TEXT, account_id BIGINT, group_id BIGINT, payload JSONB) ON COMMIT DROP;
CREATE TEMP TABLE scheduled_test_plans (id BIGINT, account_id BIGINT, enabled BOOLEAN, auto_recover BOOLEAN,
 next_run_at TIMESTAMPTZ, running_until TIMESTAMPTZ, updated_at TIMESTAMPTZ, pelican_config JSONB) ON COMMIT DROP;
CREATE TEMP TABLE scheduled_test_results (plan_id BIGINT, quality_action TEXT, pelican_config JSONB) ON COMMIT DROP;
CREATE TEMP TABLE composite_model_routes (group_id BIGINT, target_platform TEXT CHECK(target_platform IN ('openai','openai_bps','typesafe'))) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitors (provider TEXT CHECK(provider IN ('openai','openai_bps','typesafe')), account_id BIGINT) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitor_request_templates (provider TEXT CHECK(provider IN ('openai','openai_bps','typesafe'))) ON COMMIT DROP;
CREATE TEMP TABLE user_platform_quotas (platform TEXT CHECK(platform IN ('openai','openai_bps','typesafe'))) ON COMMIT DROP;
CREATE TEMP TABLE model_catalog_sources (platform TEXT, account_id BIGINT) ON COMMIT DROP;
CREATE TEMP TABLE model_catalog_jobs (account_id BIGINT) ON COMMIT DROP;
CREATE TEMP TABLE model_catalog_observations (account_id BIGINT) ON COMMIT DROP;
CREATE TEMP TABLE usage_logs (account_id BIGINT REFERENCES accounts(id), cost NUMERIC) ON COMMIT DROP;

INSERT INTO accounts (id,platform,type,status,schedulable,credentials,extra,updated_at) VALUES
 (1,'openai','oauth','active',true,'{"access_token":"keep-oauth"}','{"openai_excel_bps":true,"openai_excel_bps_auto_recover_on_403":true,"codex_ticket_harvest_enabled":true,"codex_diagnostic_monitor":{"enabled":true}}',NOW()),
 (2,'openai_bps','oauth','active',true,'{"access_token":"erase-standalone"}','{"openai_bps_credential_state":"erase","unrelated":"keep"}',NOW()),
 (3,'openai','oauth','active',true,'{"access_token":"native"}','{"openai_excel_bps":false,"codex_diagnostic_monitor":{"enabled":true}}',NOW()),
 (4,'anthropic','oauth','active',true,'{}','{}',NOW());
INSERT INTO groups (id,platform,status,updated_at) VALUES (1,'openai','active',NOW()),(2,'openai_bps','active',NOW());
INSERT INTO account_groups VALUES (1,1),(2,1),(4,2);
INSERT INTO usage_logs VALUES (1,12.3),(2,45.6);
INSERT INTO settings VALUES ('excel_bps_defaults','{}'),('excel_bps_image_mode','native'),
 ('codex_probe_template','keep challenge'),('oauth_initial_model_mappings','keep mappings');
INSERT INTO scheduled_test_plans VALUES
 (1,1,true,true,NOW(),NOW(),NOW(),'{"quality":{"action":"enable_bps","bps":{"models":["test"]},"expected_answer":"keep"}}'),
 (2,1,true,true,NOW(),NOW(),NOW(),'{"quality":{"action":"quarantine","expected_answer":"keep quality"}}'),
 (3,4,true,true,NOW(),NULL,NOW(),NULL),
 (4,2,true,true,NOW(),NOW(),NOW(),NULL);
CREATE UNIQUE INDEX scheduled_test_quality_account_scope_unique ON scheduled_test_plans (account_id,
 (CASE WHEN pelican_config->'quality'->>'action' = 'enable_bps' THEN 'bps' ELSE 'quarantine' END))
 WHERE pelican_config->'quality' IS NOT NULL;
INSERT INTO scheduled_test_results VALUES (1,'enable_bps','{"verdict":"degraded"}'),(2,'quarantine','{"verdict":"normal"}');
INSERT INTO composite_model_routes VALUES (1,'openai_bps'),(1,'openai'),(2,'openai');
INSERT INTO channel_monitors VALUES ('openai_bps',2),('openai',3);
INSERT INTO channel_monitor_request_templates VALUES ('openai_bps'),('typesafe');
INSERT INTO user_platform_quotas VALUES ('openai_bps'),('typesafe');
INSERT INTO model_catalog_sources VALUES ('openai_bps',2),('openai',1);
INSERT INTO model_catalog_jobs VALUES (2),(1);
INSERT INTO model_catalog_observations VALUES (2),(1);
`)
	require.NoError(t, err)
	contents, err := dbmigrations.FS.ReadFile("265_remove_bps_protocols.sql")
	require.NoError(t, err)
	for run := 0; run < 2; run++ {
		_, err = tx.ExecContext(ctx, string(contents))
		require.NoError(t, err, "migration must be repeatable")
	}
	var count int
	checks := []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM accounts WHERE (platform='openai_bps' AND deleted_at IS NULL) OR extra::text LIKE '%bps%'`, 0},
		{`SELECT count(*) FROM accounts WHERE id=1 AND status='disabled' AND NOT schedulable AND credentials->>'access_token'='keep-oauth' AND extra->'codex_diagnostic_monitor'->>'enabled'='true' AND extra->>'codex_ticket_harvest_enabled'='true'`, 1},
		{`SELECT count(*) FROM accounts WHERE id=2 AND platform='openai_bps' AND deleted_at IS NOT NULL AND status='disabled' AND NOT schedulable AND credentials='{}' AND extra->>'unrelated'='keep'`, 1},
		{`SELECT count(*) FROM accounts WHERE id=3 AND status='active' AND schedulable`, 1},
		{`SELECT count(*) FROM groups WHERE id=2 AND platform='openai_bps' AND deleted_at IS NOT NULL AND status='disabled'`, 1},
		{`SELECT count(*) FROM account_groups`, 1},
		{`SELECT count(*) FROM usage_logs WHERE cost IN (12.3,45.6)`, 2},
		{`SELECT count(*) FROM settings WHERE key IN ('codex_probe_template','oauth_initial_model_mappings')`, 2},
		{`SELECT count(*) FROM settings WHERE key LIKE '%bps%'`, 0},
		{`SELECT count(*) FROM scheduled_test_plans WHERE id=1 AND NOT enabled AND NOT auto_recover AND pelican_config IS NOT NULL AND pelican_config->'quality'->>'expected_answer'='keep' AND NOT (pelican_config->'quality' ? 'bps')`, 1},
		{`SELECT count(*) FROM scheduled_test_plans WHERE id IN (2,3) AND enabled`, 2},
		{`SELECT count(*) FROM scheduled_test_plans WHERE id=4 AND NOT enabled AND NOT auto_recover AND pelican_config='{}' AND next_run_at IS NULL AND running_until IS NULL`, 1},
		{`SELECT count(*) FROM scheduled_test_plans WHERE pelican_config ? 'retired'`, 0},
		{`SELECT count(*) FROM pg_indexes WHERE schemaname = pg_my_temp_schema()::regnamespace::text AND indexname LIKE 'scheduled_test_quality_%'`, 0},
		{`SELECT count(*) FROM scheduled_test_results`, 2},
		{`SELECT count(*) FROM composite_model_routes`, 1},
		{`SELECT count(*) FROM channel_monitors`, 1},
		{`SELECT count(*) FROM model_catalog_sources`, 1},
		{`SELECT count(*) FROM model_catalog_jobs`, 1},
		{`SELECT count(*) FROM model_catalog_observations`, 1},
		{`SELECT count(*) FROM scheduler_outbox WHERE account_id IN (1,2,3)`, 3},
		{`SELECT count(*) FROM scheduler_outbox WHERE account_id=2 AND payload->'group_ids'='[1]'`, 1},
		{`SELECT count(*) FROM scheduler_outbox WHERE group_id=2`, 1},
	}
	for _, check := range checks {
		require.NoError(t, tx.QueryRowContext(ctx, check.query).Scan(&count), check.query)
		require.Equal(t, check.want, count, check.query)
	}
	// Replacing constraints must retain newer supported providers as well.
	_, err = tx.ExecContext(ctx, `INSERT INTO user_platform_quotas VALUES ('typesafe')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SAVEPOINT rejected_provider`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO user_platform_quotas VALUES ('openai_bps')`)
	require.Error(t, err)
	_, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT rejected_provider`)
	require.NoError(t, err)
}

func TestRemoveCompanionExtensionsPreservesNativeAccountConfiguration(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	_, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE accounts (
 id BIGINT PRIMARY KEY, platform TEXT, type TEXT, parent_account_id BIGINT,
 status TEXT, schedulable BOOLEAN, credentials JSONB, extra JSONB,
 rate_multiplier NUMERIC, updated_at TIMESTAMPTZ
) ON COMMIT DROP;
CREATE TEMP TABLE settings (key TEXT PRIMARY KEY, value TEXT) ON COMMIT DROP;
CREATE TEMP TABLE scheduler_outbox (event_type TEXT, account_id BIGINT) ON COMMIT DROP;
CREATE TEMP TABLE usage_logs (account_id BIGINT REFERENCES accounts(id), actual_cost NUMERIC) ON COMMIT DROP;
INSERT INTO accounts VALUES
 (1,'openai','oauth',NULL,'active',true,
  '{"access_token":"keep","model_mapping":{"client":"native"},"model_mapping_mode":"aliases"}',
  '{"cost_multiplier":0.1,"cost_multiplier_auto_sync":true,"codex_diagnostic_monitor":"keep"}',1.75,NOW()),
 (2,'openai','oauth',NULL,'active',true,
  '{"access_token":"keep","model_mapping":{"client":"native"},"model_mapping_mode":"whitelist"}',
  '{"upstream_billing_probe_enabled":true,"upstream_billing_rate_sync_enabled":true,"cost_multiplier":0.25}',0.35,NOW()),
 (3,'openai','apikey',NULL,'active',true,'{"api_key":"keep","model_mapping_mode":"aliases"}','{}',1,NOW()),
 (4,'openai','oauth',1,'active',true,'{"model_mapping_mode":"aliases"}','{}',1,NOW()),
 (5,'openai','oauth',NULL,'error',false,'{"model_mapping_mode":"aliases"}','{}',1,NOW()),
 (6,'anthropic','oauth',NULL,'active',true,'{"access_token":"keep"}','{"other":true}',0.5,NOW());
INSERT INTO settings VALUES ('oauth_initial_model_mappings','{"enabled":true}'),
 ('codex_probe_template','keep diagnostic challenge'),('upstream_billing_probe_enabled','true'),
 ('priority_scheduling_v1','{"enabled":true}');
INSERT INTO usage_logs VALUES (1,12.34),(2,56.78);
`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("266_remove_companion_account_extensions.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	checks := []struct {
		sql   string
		count int
	}{
		{`SELECT count(*) FROM settings WHERE key IN ('oauth_initial_model_mappings','priority_scheduling_v1')`, 0},
		{`SELECT count(*) FROM settings WHERE key IN ('codex_probe_template','upstream_billing_probe_enabled')`, 2},
		{`SELECT count(*) FROM accounts WHERE credentials ? 'model_mapping_mode' OR extra ? 'cost_multiplier' OR extra ? 'cost_multiplier_auto_sync'`, 0},
		{`SELECT count(*) FROM accounts WHERE id=1 AND status='disabled' AND NOT schedulable AND rate_multiplier=1.75 AND credentials->>'access_token'='keep' AND credentials->'model_mapping'='{"client":"native"}' AND extra->>'codex_diagnostic_monitor'='keep'`, 1},
		{`SELECT count(*) FROM accounts WHERE id=2 AND status='active' AND schedulable AND rate_multiplier=0.35 AND credentials->'model_mapping'='{"client":"native"}' AND extra->>'upstream_billing_probe_enabled'='true' AND extra->>'upstream_billing_rate_sync_enabled'='true'`, 1},
		{`SELECT count(*) FROM accounts WHERE id IN (3,4,6) AND status='active' AND schedulable`, 3},
		{`SELECT count(*) FROM accounts WHERE id=5 AND status='error' AND NOT schedulable`, 1},
		{`SELECT count(*) FROM usage_logs WHERE actual_cost IN (12.34,56.78)`, 2},
		{`SELECT count(*) FROM scheduler_outbox`, 5},
	}
	for _, check := range checks {
		var n int
		require.NoError(t, tx.QueryRowContext(ctx, check.sql).Scan(&n), check.sql)
		require.Equal(t, check.count, n, check.sql)
	}
}
