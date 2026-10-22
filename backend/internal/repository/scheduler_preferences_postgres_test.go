//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// Use only a disposable test database: this test creates and truncates settings.
func TestSchedulerPreferencesConcurrentPostgresStartup(t *testing.T) {
	dsn := os.Getenv("SCHEDULER_REVIEW_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL DSN not supplied")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE settings (key text PRIMARY KEY, value text NOT NULL, updated_at timestamptz NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	script, err := migrations.FS.ReadFile("275_unified_scheduler_preferences.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(script))
	require.NoError(t, err)
	require.NoError(t, migrateSchedulerBusinessPreferences(ctx, db, nil), "fresh install has an empty backed-up intent")
	// A pending upgrade with a valid saved vector is finalized once across replicas.
	original := map[string]string{"openai_advanced_scheduler_enabled": "true", "openai_advanced_scheduler_weight_upstream_cost": "0.7"}
	raw, err := json.Marshal(original)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "UPDATE settings SET value=$1 WHERE key=$2", string(raw), schedulerPreferencesBackupKey)
	require.NoError(t, err)
	ready := make(chan struct{})
	failures := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { <-ready; failures <- migrateSchedulerBusinessPreferences(ctx, db, nil) })
	}
	close(ready)
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var value string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='openai_low_upstream_rate_priority_enabled'").Scan(&value))
	require.Equal(t, "true", value)
	// Once finalized, restarts cannot override a subsequent administrator edit.
	_, err = db.ExecContext(ctx, "UPDATE settings SET value='false' WHERE key='openai_low_upstream_rate_priority_enabled'")
	require.NoError(t, err)
	require.NoError(t, migrateSchedulerBusinessPreferences(ctx, db, map[string]float64{"upstream_cost": 2}))
	require.NoError(t, db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='openai_low_upstream_rate_priority_enabled'").Scan(&value))
	require.Equal(t, "false", value)
	_, err = db.ExecContext(ctx, "DELETE FROM settings WHERE key=$1", schedulerPreferencesBackupKey)
	require.NoError(t, err)
	require.ErrorContains(t, migrateSchedulerBusinessPreferences(ctx, db, nil), "backup is missing")
	var rows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM settings WHERE key=$1", schedulerPreferencesBackupKey).Scan(&rows))
	require.Zero(t, rows)
}
