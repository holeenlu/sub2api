//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

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
			_, err := tx.ExecContext(ctx, `
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
		INSERT INTO usage_logs VALUES (1,12.34);`)
			require.NoError(t, err)
			if tc.hasCurrent {
				_, err = tx.ExecContext(ctx, `INSERT INTO settings (key,value) VALUES($1,$2)`, service.SettingKeyCodexDiagnosticPromptTemplate, tc.current)
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
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, service.SettingKeyCodexDiagnosticPromptTemplate).Scan(&template))
			require.Equal(t, tc.want, template)
		})
	}
}

func TestCodexDiagnosticPostgresLifecycle(t *testing.T) {
	ctx := context.Background()
	r := &scheduledTestPlanRepository{db: integrationDB}
	var owner, key, account int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','admin') RETURNING id`, fmt.Sprintf("diagnostic-%d@example.invalid", time.Now().UnixNano())).Scan(&owner))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'Test billing key') RETURNING id`, owner, fmt.Sprintf("sk-diagnostic-%d", time.Now().UnixNano())).Scan(&key))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type) VALUES('diagnostic','openai','oauth') RETURNING id`).Scan(&account))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id=$1", key)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id=$1", owner)
	})
	connectivity, err := r.Create(ctx, &service.ScheduledTestPlan{AccountID: account, ModelID: "connectivity-only", CronExpression: "0 * * * *", MaxResults: 2, Enabled: false})
	require.NoError(t, err)
	listed, err := r.ListByAccountID(ctx, account)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	p := &service.CodexDiagnosticPlan{AccountID: account, OwnerID: owner, APIKeyID: key, Models: []string{"gpt-5.4"}, Enabled: true, IntervalMinutes: service.CodexDiagnosticDefaultIntervalMinutes}
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 1, p.Revision)
	require.NotNil(t, p.NextRunAt)
	require.WithinDuration(t, time.Now().Add(time.Hour), *p.NextRunAt, 5*time.Second)
	firstDue := *p.NextRunAt
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 1, p.Revision)
	require.Equal(t, firstDue, *p.NextRunAt)
	p.IntervalMinutes = 300
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 1, p.Revision)
	require.WithinDuration(t, time.Now().Add(5*time.Hour), *p.NextRunAt, 5*time.Second)
	// Multiple instances scanning the same due account may enqueue it once only.
	_, err = integrationDB.ExecContext(ctx, "UPDATE scheduled_test_plans SET next_run_at=NOW()-INTERVAL '1 minute' WHERE account_id=$1 AND diagnostic_config IS NOT NULL", account)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- r.EnqueueDue(ctx) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	history, err := r.ListRuns(ctx, account)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "scheduled", history[0].Source)
	normalPlans, err := r.ListByAccountID(ctx, account)
	require.NoError(t, err)
	require.Len(t, normalPlans, 1)
	require.Equal(t, connectivity.ID, normalPlans[0].ID)
	var diagnosticPlanID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT id FROM scheduled_test_plans WHERE account_id=$1 AND diagnostic_config IS NOT NULL", account).Scan(&diagnosticPlanID))
	_, err = r.GetByID(ctx, diagnosticPlanID)
	require.Error(t, err)
	require.NoError(t, r.Delete(ctx, diagnosticPlanID))
	stillThere, err := r.GetPlan(ctx, account)
	require.NoError(t, err)
	require.NotNil(t, stillThere)

	scheduledPlan, err := r.GetPlan(ctx, account)
	require.NoError(t, err)
	require.Equal(t, 300, scheduledPlan.IntervalMinutes)
	require.WithinDuration(t, time.Now().Add(5*time.Hour), *scheduledPlan.NextRunAt, 5*time.Second)
	_, err = r.Enqueue(ctx, p, "manual", "Test")
	require.ErrorIs(t, err, service.ErrDiagnosticBusy)
	running, err := r.Claim(ctx, "worker-a")
	require.NoError(t, err)
	require.NotNil(t, running)
	duplicate, err := r.Claim(ctx, "worker-b")
	require.NoError(t, err)
	require.Nil(t, duplicate)
	running.Items = []service.CodexDiagnosticItem{{Model: "gpt-5.4", Status: "normal", Probability: .99, FingerprintCommit: "bank-revision"}}
	require.NoError(t, r.SaveProgress(ctx, running))
	same, err := r.GetRun(ctx, account, running.ID)
	require.NoError(t, err)
	require.Len(t, same.Items, 1)
	_, err = r.GetRun(ctx, account+1, running.ID)
	require.ErrorIs(t, err, service.ErrDiagnosticNotFound)
	finished := time.Now()
	running.Status = "normal"
	running.FinishedAt = &finished
	require.NoError(t, r.SaveProgress(ctx, running))
	summaries, err := r.Summaries(ctx, []int64{account})
	require.NoError(t, err)
	require.Equal(t, "normal", summaries[account].Status)
	// Toggling the timer preserves the interpretation of the last check.
	p.Enabled = false
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 1, p.Revision)
	require.Nil(t, p.NextRunAt)
	summaries, err = r.Summaries(ctx, []int64{account})
	require.NoError(t, err)
	require.Equal(t, "normal", summaries[account].Status)
	require.False(t, summaries[account].Enabled)
	p.Models = []string{"gpt-5.5"}
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 2, p.Revision)
	summaries, err = r.Summaries(ctx, []int64{account})
	require.NoError(t, err)
	require.Equal(t, "unknown", summaries[account].Status)
	history, err = r.ListRuns(ctx, account)
	require.NoError(t, err)
	require.Len(t, history, 1)
	// A canceled queued run is never claimed, but remains in history.
	queued, err := r.Enqueue(ctx, p, "manual", "Test")
	require.NoError(t, err)
	require.NoError(t, r.Cancel(ctx, account, queued.ID))
	duplicate, err = r.Claim(ctx, "worker-c")
	require.NoError(t, err)
	require.Nil(t, duplicate)
	// A worker lost during a paid call is failed, never reclaimed/replayed.
	_, err = r.Enqueue(ctx, p, "manual", "Test")
	require.NoError(t, err)
	expired, err := r.Claim(ctx, "worker-expired")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE scheduled_test_results SET diagnostic_run=diagnostic_run || jsonb_build_object('lease_until',NOW()-INTERVAL '1 second') WHERE id=$1", expired.ID)
	require.NoError(t, err)
	require.NoError(t, r.EnqueueDue(ctx))
	require.ErrorIs(t, r.SaveProgress(ctx, expired), service.ErrDiagnosticNotFound)
	stale, err := r.GetRun(ctx, account, expired.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", stale.Status)
	require.Equal(t, "worker_interrupted", stale.Reason)
	duplicate, err = r.Claim(ctx, "worker-restart")
	require.NoError(t, err)
	require.Nil(t, duplicate)
	// Retention includes the newest queued run and removes only older terminal records.
	for i := 0; i < 12; i++ {
		queued, err := r.Enqueue(ctx, p, "manual", "Test")
		require.NoError(t, err)
		require.NoError(t, r.Cancel(ctx, account, queued.ID))
	}
	history, err = r.ListRuns(ctx, account)
	require.NoError(t, err)
	require.Len(t, history, 10)
	var retained int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduled_test_results r JOIN scheduled_test_plans p ON p.id=r.plan_id WHERE p.account_id=$1 AND r.diagnostic_run IS NOT NULL", account).Scan(&retained))
	require.Equal(t, 10, retained)
	_, err = r.GetRun(ctx, account, running.ID)
	require.ErrorIs(t, err, service.ErrDiagnosticNotFound)
	queued, err = r.Enqueue(ctx, p, "manual", "Test")
	require.NoError(t, err)
	history, err = r.ListRuns(ctx, account)
	require.NoError(t, err)
	require.Len(t, history, 10)
	require.Equal(t, queued.ID, history[0].ID)
	require.Equal(t, "queued", history[0].Status)

}

// Migration is one-time data conversion, not an indefinitely supported runtime
// path. A queued paid run is marked interrupted rather than replayed.
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
