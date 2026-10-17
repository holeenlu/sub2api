package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestClientTelemetryCaptureDuplicateAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "deduplicate", true: "rollback"}[fail], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := &opsRepository{db: db}
			row := &service.ClientTelemetryLog{UserID: 11, APIKeyID: 1, Client: "claude", ClientKey: "device-hash", PayloadHash: "body-hash", PayloadJSON: `{"exact":9007199254740993}`}
			mock.ExpectBegin()
			mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(regexp.QuoteMeta(insertClientTelemetrySQL)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(regexp.QuoteMeta(insertClientTelemetrySQL)).WillReturnResult(sqlmock.NewResult(0, 0))
			update := mock.ExpectExec(regexp.QuoteMeta(touchClientTelemetrySQL)).WithArgs(int64(11), "claude", "body-hash", "device-hash")
			if fail {
				update.WillReturnError(errors.New("offline"))
				mock.ExpectRollback()
			} else {
				update.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			err = repo.CaptureClientTelemetry(context.Background(), []*service.ClientTelemetryLog{row, row})
			if fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestClientTelemetryRepositoryDetailPreservesRawAndListOmitsBody(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := &opsRepository{db: db}
	now := time.Now().UTC()
	cols := []string{"id", "user_id", "api_key_id", "group_id", "client", "request_path", "event_type", "session_id", "device_id", "payload_hash", "received_count", "created_at", "last_seen_at"}
	values := []driver.Value{int64(31), int64(11), int64(1), nil, "claude", "/api/event_logging/v2/batch", "ClaudeCodeInternalEvent", "session", "device", "hash", int64(4), now, now}
	raw := `{ "number": 9007199254740993, "unknown":"keep" }`
	rows := sqlmock.NewRows(append(append([]string{}, cols...), "payload_json", "headers"))
	rows.AddRow(append(append([]driver.Value{}, values...), raw, `{"User-Agent":["claude-cli"]}`)...)
	mock.ExpectQuery("SELECT .*payload_json, headers.*WHERE id=\\$1").WithArgs(int64(31)).WillReturnRows(rows)
	row, err := repo.GetClientTelemetry(context.Background(), 31)
	require.NoError(t, err)
	require.Equal(t, raw, row.PayloadJSON)
	require.Equal(t, "claude-cli", row.Headers.Get("User-Agent"))
	mock.ExpectQuery("SELECT COUNT").WithArgs(int64(11)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	listRows := sqlmock.NewRows(cols).AddRow(values...)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+clientTelemetryColumns)+" FROM ops_client_telemetry_logs").WithArgs(int64(11), 100, 0).WillReturnRows(listRows)
	userID := int64(11)
	list, err := repo.ListClientTelemetry(context.Background(), &service.ClientTelemetryLogFilter{UserID: &userID, PageSize: 1000})
	require.NoError(t, err)
	require.Equal(t, 100, list.PageSize)
	require.Len(t, list.Items, 1)
	require.Empty(t, list.Items[0].PayloadJSON)
	require.Nil(t, list.Items[0].Headers)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountTelemetryValidationUsesSharedPersistenceBoundary(t *testing.T) {
	copy := map[string]any{"client": "claude", "payload_json": `{"event_type":"ClaudeCodeInternalEvent","event_data":{"device_id":"account-device","original":9007199254740993}}`, "headers": map[string][]string{"User-Agent": {"cli"}}}
	require.NoError(t, validateAccountWriteFields(nil, map[string]any{service.AccountClientTelemetryExtraKey: copy}))
	copy["sample_id"] = int64(5)
	require.Error(t, validateAccountWriteFields(nil, map[string]any{service.AccountClientTelemetryExtraKey: copy}))
}
