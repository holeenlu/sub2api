//go:build integration

package repository

import (
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	"testing"
)

// Historical migration remains supported; no retired collector is invoked.
func TestClientTelemetryLocalOnlyMigrationPreservesSamples(t *testing.T) {
	ctx := t.Context()
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE ops_client_telemetry_logs (LIKE public.ops_client_telemetry_logs INCLUDING DEFAULTS) ON COMMIT DROP;
		ALTER TABLE ops_client_telemetry_logs ADD COLUMN account_id BIGINT, ADD COLUMN status TEXT,
		 ADD COLUMN upstream_status INTEGER, ADD COLUMN error_code TEXT;
		INSERT INTO ops_client_telemetry_logs
		 (user_id, api_key_id, client, payload_hash, request_path, event_type, payload_json, received_count, account_id, status, upstream_status)
		 VALUES (11, 1, 'claude', 'hash', '/api/event_logging/batch', 'ClaudeCodeInternalEvent',
		 '{ "unknown": 9007199254740993 }', 8, 7, 'upstream_responded', 202)`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("283_client_telemetry_local_only.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	var raw string
	var count, retired int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT payload_json, received_count FROM ops_client_telemetry_logs`).Scan(&raw, &count))
	require.Equal(t, `{ "unknown": 9007199254740993 }`, raw)
	require.Equal(t, 8, count)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_attribute
	 WHERE attrelid = 'ops_client_telemetry_logs'::regclass AND NOT attisdropped
	 AND attname IN ('account_id', 'status', 'upstream_status', 'error_code')`).Scan(&retired))
	require.Zero(t, retired)
}
