//go:build integration

package repository

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const nativeGatewayRetirementFixture = `
		CREATE TEMP TABLE accounts (id BIGINT PRIMARY KEY, credentials JSONB, extra JSONB, updated_at TIMESTAMPTZ) ON COMMIT DROP;
		CREATE TEMP TABLE settings (key TEXT PRIMARY KEY, value TEXT) ON COMMIT DROP;
		CREATE TEMP TABLE scheduler_outbox (event_type TEXT, account_id BIGINT) ON COMMIT DROP;
		CREATE TEMP TABLE codex_ticket_invalidations (id BIGINT, reason_code TEXT, original_ticket TEXT, original_cookie TEXT,
		 returned_ticket TEXT NOT NULL, returned_cookie TEXT, returned_set_cookies JSONB) ON COMMIT DROP;
		CREATE TEMP TABLE scheduled_test_plans (id BIGINT, account_id BIGINT, diagnostic_config JSONB) ON COMMIT DROP;
		CREATE TEMP TABLE scheduled_test_results (plan_id BIGINT, diagnostic_run JSONB) ON COMMIT DROP;
		CREATE TEMP TABLE usage_logs (account_id BIGINT REFERENCES accounts(id), actual_cost NUMERIC) ON COMMIT DROP;
		INSERT INTO accounts VALUES
		 (1,'{"access_token":"keep-token","model_mapping":{"client":"native"}}',
		  '{"codex_turn_ticket:gpt-6-astra":{"cookie":"erase"},"codex_ticket_harvest_enabled":true,"codex_allow_without_ticket":false,
		    "openai_apikey_codex_identity":true,"openai_oauth_ws_sse_acceleration":true,"model_catalog_snapshot":{},
		    "base_rpm":100,"openai_request_timezone":"Asia/Taipei","custom":"keep"}',NOW()),
		 (2,'{}','{"custom":"untouched"}',NOW());
		INSERT INTO settings VALUES ('openai_codex_ticket_enabled','true'),('openai_codex_ticket_proxy_pool','{}'),
		 ('openai_codex_ticket_prompt_template','keep-template'),('codex_modeltrace_bank_v1','keep-bank'),
		 ('account_scheduling_thresholds','{"anthropic":90,"anthropic_fable":60}'),
		 ('model_catalog_settings','{}'),('model_catalog_registry','[]'),('upstream_failover_status_codes','500,503'),('openai_codex_version_override','keep-native');
		INSERT INTO codex_ticket_invalidations VALUES (7,'keep-audit','erase','erase','erase','erase','["erase"]');
		INSERT INTO scheduled_test_plans VALUES (9,1,'{"models":["gpt-6-astra"],"enabled":true}');
		INSERT INTO scheduled_test_results VALUES (9,'{"status":"normal","probability":0.99}');
		INSERT INTO usage_logs VALUES (1,12.34);`

func TestNativeGatewayRetirementPreservesDiagnosticsAndBilling(t *testing.T) {
	for _, tc := range []struct {
		name       string
		hasCurrent bool
		current    string
		want       string
	}{
		{name: "migrate saved template", want: "keep-template"},
		{name: "keep current template", hasCurrent: true, current: "current-template", want: "current-template"},
		{name: "keep explicit default", hasCurrent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx := testTx(t)
			_, err := tx.ExecContext(ctx, nativeGatewayRetirementFixture)
			require.NoError(t, err)
			if tc.hasCurrent {
				_, err = tx.ExecContext(ctx, `INSERT INTO settings (key,value) VALUES($1,$2)`, "openai_codex_diagnostic_prompt_template", tc.current)
				require.NoError(t, err)
			}
			contents, err := os.ReadFile("../../migrations/268_retire_fork_gateway_runtime.sql")
			require.NoError(t, err)
			for range 2 {
				_, err = tx.ExecContext(ctx, string(contents))
				require.NoError(t, err)
			}
			for _, check := range []struct {
				query string
				count int
			}{
				{`SELECT count(*) FROM accounts WHERE id=1 AND credentials->>'access_token'='keep-token' AND credentials->'model_mapping'='{"client":"native"}' AND extra='{"base_rpm":100,"openai_request_timezone":"Asia/Taipei","custom":"keep"}'`, 1},
				{`SELECT count(*) FROM accounts WHERE id=2 AND extra='{"custom":"untouched"}'`, 1},
				{`SELECT count(*) FROM settings`, 4},
				{`SELECT count(*) FROM settings WHERE key ~ '^openai_codex_ticket_'`, 0},
				{`SELECT count(*) FROM settings WHERE key='account_scheduling_thresholds' AND value::jsonb='{"anthropic":90}'`, 1},
				{`SELECT count(*) FROM settings WHERE key IN ('openai_codex_diagnostic_prompt_template','codex_modeltrace_bank_v1','openai_codex_version_override')`, 3},
				{`SELECT count(*) FROM codex_ticket_invalidations WHERE id=7 AND reason_code='keep-audit' AND original_ticket IS NULL AND original_cookie IS NULL AND returned_ticket='' AND returned_cookie IS NULL AND returned_set_cookies IS NULL`, 1},
				{`SELECT count(*) FROM scheduled_test_plans WHERE diagnostic_config='{"models":["gpt-6-astra"],"enabled":true}'`, 1},
				{`SELECT count(*) FROM scheduled_test_results WHERE diagnostic_run='{"status":"normal","probability":0.99}'`, 1},
				{`SELECT count(*) FROM usage_logs WHERE actual_cost=12.34`, 1},
				{`SELECT count(*) FROM scheduler_outbox WHERE account_id=1`, 1},
			} {
				var count int
				require.NoError(t, tx.QueryRowContext(ctx, check.query).Scan(&count), check.query)
				require.Equal(t, check.count, count, check.query)
			}
			var template string
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, "openai_codex_diagnostic_prompt_template").Scan(&template))
			require.Equal(t, tc.want, template)
		})
	}
}

func TestDiagnosticMigrationPreservesEvidenceAndConnectivity(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "CREATE SCHEMA diagnostic_migration_fixture")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "SET LOCAL search_path=diagnostic_migration_fixture,public")
	require.NoError(t, err)
	// Minimal related rows keep this fixture isolated from other repository tests.
	_, err = tx.ExecContext(ctx, "CREATE TABLE accounts(id bigint PRIMARY KEY); CREATE TABLE users(id bigint PRIMARY KEY); CREATE TABLE api_keys(id bigint PRIMARY KEY); INSERT INTO accounts VALUES(1); INSERT INTO users VALUES(1); INSERT INTO api_keys VALUES(1)")
	require.NoError(t, err)
	for _, name := range []string{"066_add_scheduled_test_tables.sql", "264_codex_diagnostic_monitor.sql"} {
		raw, err := os.ReadFile("../../migrations/" + name)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(raw))
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(ctx, `
 INSERT INTO scheduled_test_plans(id,account_id,model_id,enabled) VALUES(1,1,'connectivity-model',true);
 INSERT INTO scheduled_test_results(plan_id,status,response_text) VALUES(1,'success','keep connectivity result');
 INSERT INTO codex_diagnostic_plans(account_id,owner_id,api_key_id,models,enabled,interval_minutes)
 VALUES(1,1,1,'["gpt-5.4"]',true,300);
 INSERT INTO codex_diagnostic_runs(account_id,owner_id,api_key_id,api_key_name,plan_revision,models,source,status,items)
 VALUES(1,1,1,'test-key-name',1,'["gpt-5.4"]','manual','normal','[{"model":"gpt-5.4","status":"normal","probability":0.99}]'),
 (1,1,1,'test-key-name',1,'["gpt-5.4"]','scheduled','running','[]');`)
	require.NoError(t, err)
	// Explicit ID above is only for the fixture; advance its sequence.
	_, err = tx.ExecContext(ctx, "SELECT setval('scheduled_test_plans_id_seq',1)")
	require.NoError(t, err)
	raw, err := os.ReadFile("../../migrations/267_unify_diagnostic_scheduled_tests.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(raw))
	require.NoError(t, err)
	var normal, interrupted, interval int
	var legacy, connectivity string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduled_test_results WHERE status='normal' AND diagnostic_run->'items'->0->>'model'='gpt-5.4'").Scan(&normal))
	require.Equal(t, 1, normal)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduled_test_results WHERE status='failed' AND error_message='worker_interrupted'").Scan(&interrupted))
	require.Equal(t, 1, interrupted)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT (diagnostic_config->>'interval_minutes')::int FROM scheduled_test_plans WHERE diagnostic_config IS NOT NULL").Scan(&interval))
	require.Equal(t, 300, interval)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COALESCE(to_regclass('diagnostic_migration_fixture.codex_diagnostic_plans')::text,'removed')").Scan(&legacy))
	require.Equal(t, "removed", legacy)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT response_text FROM scheduled_test_results WHERE diagnostic_run IS NULL").Scan(&connectivity))
	require.Equal(t, "keep connectivity result", connectivity)
}

func TestFableThresholdUpgradePreservesExistingConfiguration(t *testing.T) {
	ctx := context.Background()
	prepare, err := os.ReadFile("../../migrations/267_preserve_fable_threshold.sql")
	require.NoError(t, err)
	retire, err := os.ReadFile("../../migrations/268_retire_fork_gateway_runtime.sql")
	require.NoError(t, err)
	restore, err := os.ReadFile("../../migrations/269_restore_fable_threshold.sql")
	require.NoError(t, err)
	for _, tc := range []struct {
		name                          string
		alreadyUpgraded, newerDefault bool
		want                          string
	}{
		{name: "preserve old value", want: "60"}, {name: "keep newer explicit default", newerDefault: true, want: "100"}, {name: "already upgraded does not invent deleted value", alreadyUpgraded: true, want: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := testTx(t)
			_, err := tx.ExecContext(ctx, nativeGatewayRetirementFixture)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, "CREATE TEMP TABLE schema_migrations(filename TEXT PRIMARY KEY) ON COMMIT DROP")
			require.NoError(t, err)
			if tc.alreadyUpgraded {
				_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES('268_retire_fork_gateway_runtime.sql'); UPDATE settings SET value=(value::jsonb-'anthropic_fable')::text WHERE key='account_scheduling_thresholds'")
				require.NoError(t, err)
			}
			for range 2 {
				_, err = tx.ExecContext(ctx, string(prepare))
				require.NoError(t, err)
			}
			if !tc.alreadyUpgraded {
				_, err = tx.ExecContext(ctx, string(retire))
				require.NoError(t, err)
			}
			if tc.newerDefault {
				_, err = tx.ExecContext(ctx, "UPDATE settings SET value=jsonb_set(value::jsonb,'{anthropic_fable}','100')::text WHERE key='account_scheduling_thresholds'")
				require.NoError(t, err)
			}
			for range 2 {
				_, err = tx.ExecContext(ctx, string(restore))
				require.NoError(t, err)
			}
			var value string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT COALESCE(value::jsonb->>'anthropic_fable','missing') FROM settings WHERE key='account_scheduling_thresholds'").Scan(&value))
			require.Equal(t, tc.want, value)
			var backups int
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM settings WHERE key='migration_fable_threshold_before_268'").Scan(&backups))
			require.Zero(t, backups)
			var ordinary string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT value::jsonb->>'anthropic' FROM settings WHERE key='account_scheduling_thresholds'").Scan(&ordinary))
			require.Equal(t, "90", ordinary)
		})
	}
}
