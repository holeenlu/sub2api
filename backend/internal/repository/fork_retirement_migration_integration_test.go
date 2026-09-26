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
	for _, table := range []string{"accounts", "groups", "users", "channel_monitors", "composite_model_routes", "scheduled_test_plans", "announcements", "scheduler_outbox", "settings"} {
		_, err := tx.ExecContext(ctx, fmt.Sprintf("CREATE TEMP TABLE %s (LIKE public.%s INCLUDING ALL) ON COMMIT DROP", table, table))
		require.NoError(t, err)
	}
	_, err := tx.ExecContext(ctx, `
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
