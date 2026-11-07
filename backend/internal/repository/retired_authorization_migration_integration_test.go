//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration290RemovesSavedAuthorizationAndPreservesAccountsAndBilling(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	legacy, err := dbmigrations.FS.ReadFile("284_openai_siwc_grants.sql")
	require.NoError(t, err)
	cleanup, err := dbmigrations.FS.ReadFile("290_remove_openai_siwc_authorization.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(legacy))
	require.NoError(t, err)

	// Include disabled, malformed and soft-deleted metadata: retirement must not
	// leave a registration behind merely because it cannot currently be used.
	var accountIDs []int64
	expectedAccounts := map[int64]string{}
	for _, extra := range []string{
		`{"openai_siwc":true,"note":"keep","openai_excel":true}`,
		`{"openai_siwc":false,"note":"disconnected"}`,
		`{"openai_siwc":null,"note":"deleted"}`,
		`{"openai_siwc":{"stale":true},"note":"malformed"}`,
		`{"note":"native-only"}`,
	} {
		var id int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, credentials, extra, deleted_at)
VALUES ('retired-authorization', 'openai', 'oauth',
 '{"access_token":"native-access","refresh_token":"native-refresh","chatgpt_account_id":"native-account","auth_mode":"chatgpt","plan_type":"plus"}'::jsonb,
 $1::jsonb, CASE WHEN ($1::jsonb)->>'note' = 'deleted' THEN NOW() ELSE NULL END)
RETURNING id`, extra).Scan(&id))
		accountIDs = append(accountIDs, id)
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (to_jsonb(a) || jsonb_build_object('extra', a.extra - 'openai_siwc'))::text
FROM accounts a WHERE id = $1`, id).Scan(&extra))
		expectedAccounts[id] = extra
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO openai_siwc_installation (host_id) VALUES ('urn:uuid:retired-host')")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
INSERT INTO openai_siwc_grants (account_id, registration_ciphertext, credentials_ciphertext)
VALUES ($1, 'encrypted-registration', 'encrypted-access-refresh-id-tokens'),
       ($2, 'disconnected-registration', '')`, accountIDs[0], accountIDs[1])
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "INSERT INTO openai_excel_grants (account_id, ciphertext) VALUES ($1, 'encrypted-bps-grant')", accountIDs[0])
	require.NoError(t, err)

	var userID, keyID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO users (email, password_hash, balance)
VALUES ('retired-authorization@example.com', 'test', 123.45) RETURNING id`).Scan(&userID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO api_keys (user_id, key, name) VALUES ($1, 'retired-authorization-key', 'test')
RETURNING id`, userID).Scan(&keyID))
	for _, mode := range []any{"native", "bps", "siwc", nil} {
		_, err = tx.ExecContext(ctx, `
INSERT INTO usage_logs (user_id, api_key_id, account_id, request_id, model,
 authorization_mode, total_cost, actual_cost, billing_status)
VALUES ($1, $2, $3, COALESCE($4::text, 'unknown'), 'test-model', $4, 1.25, 1.25, 'settled')`,
			userID, keyID, accountIDs[0], mode)
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO usage_billing_events (request_id, api_key_id, request_fingerprint, payload, status)
VALUES ('retired-authorization', $1, 'keep-fingerprint',
 '{"authorization_mode":"siwc","balance_cost":1.25}'::jsonb, 'pending');`, keyID)
	require.NoError(t, err)

	snapshots := []string{
		"SELECT jsonb_agg(to_jsonb(t) ORDER BY account_id)::text FROM openai_excel_grants t",
		"SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM usage_logs t",
		"SELECT jsonb_agg(to_jsonb(t) ORDER BY request_id, api_key_id)::text FROM usage_billing_events t",
		"SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM users t",
		"SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM api_keys t",
	}
	before := make([]string, len(snapshots))
	for i, query := range snapshots {
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&before[i]))
	}

	// A late DDL failure must roll back both the earlier metadata update and
	// grant-table removal. The migration runner supplies the enclosing transaction.
	_, err = tx.ExecContext(ctx, `CREATE VIEW retired_authorization_dependency AS
SELECT host_id FROM openai_siwc_installation; SAVEPOINT retirement;`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(cleanup))
	require.ErrorContains(t, err, "depend on it")
	_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT retirement")
	require.NoError(t, err)
	var grantCount, flaggedCount int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_siwc_grants").Scan(&grantCount))
	require.Equal(t, 2, grantCount)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts WHERE extra ? 'openai_siwc'").Scan(&flaggedCount))
	require.Equal(t, 4, flaggedCount)
	_, err = tx.ExecContext(ctx, "DROP VIEW retired_authorization_dependency")
	require.NoError(t, err)

	for range 2 {
		_, err = tx.ExecContext(ctx, string(cleanup))
		require.NoError(t, err)
		for _, table := range []string{"openai_siwc_grants", "openai_siwc_installation"} {
			var name sql.NullString
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT to_regclass($1)::text", table).Scan(&name))
			require.False(t, name.Valid, "retired table %s still exists", table)
		}
		for id, expected := range expectedAccounts {
			var actual string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT to_jsonb(a)::text FROM accounts a WHERE id=$1", id).Scan(&actual))
			require.JSONEq(t, expected, actual)
		}
		for i, query := range snapshots {
			var actual string
			require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&actual))
			require.JSONEq(t, before[i], actual)
		}
	}
	_, err = tx.ExecContext(ctx, "SAVEPOINT immutable_history")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET authorization_mode='native' WHERE api_key_id=$1 AND authorization_mode='siwc'", keyID)
	require.ErrorContains(t, err, "usage authorization mode is immutable")
	_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT immutable_history")
	require.NoError(t, err)
}

func TestMigration290WorksWithoutSavedAuthorization(t *testing.T) {
	for _, legacyTables := range []bool{true, false} {
		name := "tables_absent"
		if legacyTables {
			name = "empty_tables"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			tx := testTx(t)
			if legacyTables {
				legacy, err := dbmigrations.FS.ReadFile("284_openai_siwc_grants.sql")
				require.NoError(t, err)
				_, err = tx.ExecContext(ctx, string(legacy))
				require.NoError(t, err)
			}
			cleanup, err := dbmigrations.FS.ReadFile("290_remove_openai_siwc_authorization.sql")
			require.NoError(t, err)
			for range 2 {
				_, err = tx.ExecContext(ctx, string(cleanup))
				require.NoError(t, err)
			}
		})
	}
}
