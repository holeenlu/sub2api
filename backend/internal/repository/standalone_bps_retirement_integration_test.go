//go:build integration

package repository

import (
	"context"
	"fmt"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMigration254RetiresOnlyStandaloneBPS(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	for _, table := range []string{"accounts", "groups", "channel_monitors", "composite_model_routes", "scheduled_test_plans", "scheduler_outbox"} {
		_, err := tx.ExecContext(ctx, fmt.Sprintf("CREATE TEMP TABLE %s (LIKE public.%s INCLUDING ALL) ON COMMIT DROP", table, table))
		require.NoError(t, err)
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO accounts(id,name,platform,type,credentials,extra,status,schedulable,error_message) VALUES
(1,'standalone','openai_bps','oauth','{"access_token":"fixture"}','{"history":"keep"}','active',true,'keep diagnostic'),
(2,'OAuth Excel','openai','oauth','{"refresh_token":"fixture"}','{"openai_excel_bps":true,"openai_excel_bps_models":["gpt-test"],"openai_oauth_rpm_limit":7,"codex_ticket_harvest_enabled":true}','active',true,NULL),
(3,'native','openai','oauth','{}','{}','active',true,NULL),
(4,'already retired','openai_bps','oauth','{}','{}','disabled',false,NULL);
INSERT INTO groups(id,name,platform) VALUES (1,'standalone','openai_bps'),(2,'OpenAI','openai');
INSERT INTO channel_monitors(id,name,provider,account_id,endpoint,api_key_encrypted,primary_model,interval_seconds,created_by) VALUES
(1,'standalone','openai_bps',1,'https://example.invalid','fixture','gpt-test',60,1),
(2,'legacy account monitor','openai',1,'https://example.invalid','fixture','gpt-test',60,1),
(3,'OAuth Excel','openai',2,'https://example.invalid','fixture','gpt-test',60,1);
INSERT INTO composite_model_routes(id,group_id,public_model,target_platform) VALUES
(1,2,'legacy','openai_bps'),(2,2,'normal','openai');
INSERT INTO scheduled_test_plans(id,account_id,enabled,auto_recover,next_run_at,running_until) VALUES
(1,1,true,true,NOW(),NOW()+INTERVAL '1 hour'),(2,2,true,true,NOW(),NULL),(3,3,true,false,NOW(),NULL);
`)
	require.NoError(t, err)
	sql, err := dbmigrations.FS.ReadFile("254_retire_standalone_bps.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(sql))
		require.NoError(t, err)
		checks := []struct {
			query string
			count int
		}{
			{`SELECT COUNT(*) FROM accounts`, 4},
			{`SELECT COUNT(*) FROM accounts WHERE (id IN (1,4) AND status='disabled' AND NOT schedulable) OR (id IN (2,3) AND status='active' AND schedulable)`, 4},
			{`SELECT COUNT(*) FROM groups WHERE (id=1 AND status='disabled') OR (id=2 AND status='active')`, 2},
			{`SELECT COUNT(*) FROM channel_monitors WHERE (id IN (1,2) AND NOT enabled) OR (id=3 AND enabled)`, 3},
			{`SELECT COUNT(*) FROM composite_model_routes WHERE (id=1 AND NOT enabled) OR (id=2 AND enabled)`, 2},
			{`SELECT COUNT(*) FROM scheduled_test_plans WHERE (id=1 AND NOT enabled AND NOT auto_recover AND next_run_at IS NULL AND running_until IS NULL) OR (id=2 AND enabled AND auto_recover AND next_run_at IS NOT NULL) OR (id=3 AND enabled AND NOT auto_recover AND next_run_at IS NOT NULL)`, 3},
			{`SELECT COUNT(*) FROM scheduler_outbox WHERE event_type='account_changed' AND account_id=1`, 1},
			{`SELECT COUNT(*) FROM scheduler_outbox WHERE event_type='group_changed' AND group_id=1`, 1},
			{`SELECT COUNT(*) FROM scheduler_outbox`, 2},
		}
		for _, check := range checks {
			var count int
			require.NoError(t, tx.QueryRowContext(ctx, check.query).Scan(&count))
			require.Equal(t, check.count, count, check.query)
		}
	}
	var credentials, extra, diagnostic string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials::text,extra::text,error_message FROM accounts WHERE id=1`).Scan(&credentials, &extra, &diagnostic))
	require.JSONEq(t, `{"access_token":"fixture"}`, credentials)
	require.JSONEq(t, `{"history":"keep"}`, extra)
	require.Equal(t, "keep diagnostic", diagnostic)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT credentials::text,extra::text FROM accounts WHERE id=2`).Scan(&credentials, &extra))
	require.JSONEq(t, `{"refresh_token":"fixture"}`, credentials)
	require.JSONEq(t, `{"openai_excel_bps":true,"openai_excel_bps_models":["gpt-test"],"openai_oauth_rpm_limit":7,"codex_ticket_harvest_enabled":true}`, extra)
}
