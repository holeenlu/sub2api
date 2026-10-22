package repository

import (
	"context"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// Keep the provider-health status for existing filters, but deliver the stored
// request outcome as a separate field in both list and detail responses.
func TestOpsErrorQueriesPreserveRecoveredRequestStatus(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, requestStatus := range []driver.Value{int64(200), nil} {
			t.Run(fmt.Sprintf("detail=%v/status=%v", detail, requestStatus), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer db.Close()
				repo := NewOpsRepository(db).(*opsRepository)
				now := time.Unix(1791557966, 0)
				message := "Recovered upstream error 503: overloaded"
				var values []driver.Value
				if detail {
					values = []driver.Value{int64(4712), now, "upstream", "upstream_error", "provider", "upstream_http", "P2", int64(503), "openai", "gpt-6.1-sol", false, nil, nil, "client-id", "request-id", message, "", int64(503), "overloaded", "", "[]", false, nil, "", nil, nil, "", nil, "", nil, "/v1/responses", true, "/v1/responses", "/v1/responses", "gpt-6.1-sol", "gpt-6.1-sol", int64(2), "test-agent", nil, nil, nil, nil, nil, "", "", nil, requestStatus}
				} else {
					mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM ops_error_logs").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
					values = []driver.Value{int64(4712), now, "upstream", "upstream_error", "provider", "upstream_http", "P2", int64(503), "openai", "gpt-6.1-sol", false, nil, nil, "", "client-id", "request-id", message, nil, "", nil, nil, "", nil, "", nil, "/v1/responses", true, "/v1/responses", "/v1/responses", "gpt-6.1-sol", "gpt-6.1-sol", "test-agent", int64(2), "", nil, requestStatus}
				}
				columns := make([]string, len(values))
				for i := range columns {
					columns[i] = fmt.Sprint("column_", i)
				}
				mock.ExpectQuery(`(?s)SELECT.*COALESCE\(e.upstream_status_code, e.status_code, 0\).*ak.deleted_at,\s+e.status_code\s+FROM ops_error_logs`).WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
				var final *int
				if detail {
					row, err := repo.GetErrorLogByID(context.Background(), 4712)
					require.NoError(t, err)
					require.Equal(t, 503, row.StatusCode)
					require.Equal(t, 503, *row.UpstreamStatusCode)
					require.Equal(t, message, row.Message)
					final = row.RequestStatusCode
				} else {
					list, err := repo.ListErrorLogs(context.Background(), nil)
					require.NoError(t, err)
					require.Len(t, list.Errors, 1)
					require.Equal(t, 503, list.Errors[0].StatusCode)
					require.Equal(t, message, list.Errors[0].Message)
					final = list.Errors[0].RequestStatusCode
				}
				if requestStatus == nil {
					require.Nil(t, final)
				} else {
					require.NotNil(t, final)
					require.Equal(t, int(requestStatus.(int64)), *final)
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}
