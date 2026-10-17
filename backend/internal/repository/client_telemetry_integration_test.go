//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

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

func TestClientTelemetryPostgresDeduplicatesConcurrentClientsAndPayloads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := &opsRepository{db: integrationDB}
	userID := time.Now().UnixNano()
	defer integrationDB.ExecContext(context.Background(), "DELETE FROM ops_client_telemetry_logs WHERE user_id IN ($1,$2)", userID, userID+1)
	first := &service.ClientTelemetryLog{UserID: userID, APIKeyID: 1, Client: "claude", ClientKey: "device-one", PayloadHash: "first-body", DeviceID: "client-device", EventType: "ClaudeCodeInternalEvent", PayloadJSON: `{ "unknown": 9007199254740993 }`}
	require.NoError(t, repo.CaptureClientTelemetry(ctx, []*service.ClientTelemetryLog{first}))
	list, err := repo.ListClientTelemetry(ctx, &service.ClientTelemetryLogFilter{UserID: &userID})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	firstID := list.Items[0].ID

	const workers = 16
	var wg sync.WaitGroup
	failures := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			row := *first
			row.APIKeyID = int64(index + 2) // A user's different keys do not create duplicate clients.
			row.PayloadHash = fmt.Sprintf("changed-body-%d", index)
			row.PayloadJSON = fmt.Sprintf(`{"new_event":%d}`, index)
			failures <- repo.CaptureClientTelemetry(ctx, []*service.ClientTelemetryLog{&row})
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	stored, err := repo.GetClientTelemetry(ctx, firstID)
	require.NoError(t, err)
	require.Equal(t, int64(workers+1), stored.ReceivedCount)
	require.Equal(t, first.PayloadJSON, stored.PayloadJSON)
	require.Equal(t, int64(1), stored.APIKeyID)

	noDevice := *first
	noDevice.ClientKey, noDevice.DeviceID, noDevice.PayloadHash = "", "", "anonymous-body"
	require.NoError(t, repo.CaptureClientTelemetry(ctx, []*service.ClientTelemetryLog{&noDevice, &noDevice}))
	list, err = repo.ListClientTelemetry(ctx, &service.ClientTelemetryLogFilter{UserID: &userID})
	require.NoError(t, err)
	require.Len(t, list.Items, 2, "concurrent same-device uploads must not create extra rows")
	require.Equal(t, int64(2), list.Items[0].ReceivedCount, "matching payloads without a device are deduplicated")
	otherUser := *first
	otherUser.UserID++
	require.NoError(t, repo.CaptureClientTelemetry(ctx, []*service.ClientTelemetryLog{&otherUser}))
	otherList, err := repo.ListClientTelemetry(ctx, &service.ClientTelemetryLogFilter{UserID: &otherUser.UserID})
	require.NoError(t, err)
	require.Len(t, otherList.Items, 1, "different users must not share samples")
	differentClient := *first
	differentClient.ClientKey, differentClient.PayloadHash = "device-two", "different-body"
	require.NoError(t, repo.CaptureClientTelemetry(ctx, []*service.ClientTelemetryLog{&differentClient}))
	list, err = repo.ListClientTelemetry(ctx, &service.ClientTelemetryLogFilter{UserID: &userID, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(3), list.Total)
	for _, row := range list.Items {
		require.Empty(t, row.PayloadJSON)
	}
}

func TestAccountTelemetryCopySurvivesSourceDeletion(t *testing.T) {
	ctx := context.Background()
	logs := &opsRepository{db: integrationDB}
	body := `{ "event_type":"ClaudeCodeInternalEvent", "event_data":{"device_id":"original-device","unknown":9007199254740993} }`
	userID := time.Now().UnixNano()
	require.NoError(t, logs.CaptureClientTelemetry(ctx, []*service.ClientTelemetryLog{{UserID: userID, APIKeyID: 1, Client: "claude", PayloadHash: "copy-test", PayloadJSON: body}}))
	defer integrationDB.ExecContext(ctx, "DELETE FROM ops_client_telemetry_logs WHERE user_id=$1", userID)
	list, err := logs.ListClientTelemetry(ctx, &service.ClientTelemetryLogFilter{UserID: &userID})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	sample, err := logs.GetClientTelemetry(ctx, list.Items[0].ID)
	require.NoError(t, err)
	account := &service.Account{Name: "independent-telemetry", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "test-key"}, Extra: map[string]any{
		service.AccountClientTelemetryExtraKey: map[string]any{"client": sample.Client, "payload_json": sample.PayloadJSON, "headers": sample.Headers}, "unrelated": true,
	}}
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	require.NoError(t, repo.Create(ctx, account))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
	})
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM ops_client_telemetry_logs WHERE id=$1", sample.ID)
	require.NoError(t, err)
	stored, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	copy := stored.Extra[service.AccountClientTelemetryExtraKey].(map[string]any)
	require.Equal(t, body, copy["payload_json"])
	require.Len(t, copy, 3)
	require.NotContains(t, copy, "sample_id")
	delete(stored.Extra, service.AccountClientTelemetryExtraKey)
	require.NoError(t, repo.Update(ctx, stored))
	stored, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.NotContains(t, stored.Extra, service.AccountClientTelemetryExtraKey)
	require.Equal(t, true, stored.Extra["unrelated"])
}
