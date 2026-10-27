package repository

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpsRepositorySystemMetricsWindowPreservesValuesAndNulls(t *testing.T) {
	db, mock := newSQLMock(t)
	db.SetMaxOpenConns(1)
	start := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	mock.ExpectQuery("COALESCE\\(COUNT\\(\\*\\)").WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"count", "tokens"}).AddRow(120, 6000))
	columns := []string{"p50", "p90", "p95", "p99", "avg", "max"}
	mock.ExpectQuery("ORDER BY duration_ms").WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(10.4, 20.5, 30.6, 40.1, 25.55, int64(50)))
	mock.ExpectQuery("ORDER BY first_token_ms").WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(make([]driver.Value, 6)...))
	mock.ExpectQuery("AND is_count_tokens = FALSE").WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"total", "business", "sla", "upstream", "429", "529"}).AddRow(5, 2, 3, 1, 1, 1))
	mock.ExpectQuery("jsonb_array_elements").WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"switches"}).AddRow(2))
	input, err := (&opsRepository{db: db}).GetSystemMetricsWindow(t.Context(), start, end)
	require.NoError(t, err)
	require.Equal(t, end, input.CreatedAt)
	require.Equal(t, 1, input.WindowMinutes)
	require.Equal(t, int64(120), input.SuccessCount)
	require.Equal(t, int64(6000), input.TokenConsumed)
	require.Equal(t, int64(5), input.ErrorCountTotal)
	require.Equal(t, int64(2), input.BusinessLimitedCount)
	require.Equal(t, int64(3), input.ErrorCountSLA)
	require.Equal(t, int64(2), input.AccountSwitchCount)
	require.Equal(t, 2.1, *input.QPS)
	require.Equal(t, 100.0, *input.TPS)
	require.Equal(t, 10, *input.DurationP50Ms)
	require.Equal(t, 21, *input.DurationP90Ms)
	require.Equal(t, 25.6, *input.DurationAvgMs)
	require.Nil(t, input.TTFTP50Ms)
	require.Nil(t, input.TTFTAvgMs)
	require.Nil(t, input.TTFTMaxMs)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Zero(t, db.Stats().InUse)
}

func TestOpsRepositoryCollectedErrorCountsExcludesCountTokens(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := &opsRepository{db: db}
	start := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectQuery(`(?s)FROM ops_error_logs\s+WHERE created_at >= \$1 AND created_at < \$2\s+AND is_count_tokens = FALSE`).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"error_total",
			"business_limited",
			"error_sla",
			"upstream_excl",
			"upstream_429",
			"upstream_529",
		}).AddRow(int64(5), int64(2), int64(3), int64(1), int64(1), int64(1)))

	errorTotal, businessLimited, errorSLA, upstreamExcl429529, upstream429, upstream529, err := repo.queryCollectedErrorCounts(context.Background(), start, end)
	require.NoError(t, err)
	require.Equal(t, int64(5), errorTotal)
	require.Equal(t, int64(2), businessLimited)
	require.Equal(t, int64(3), errorSLA)
	require.Equal(t, int64(1), upstreamExcl429529)
	require.Equal(t, int64(1), upstream429)
	require.Equal(t, int64(1), upstream529)
	require.NoError(t, mock.ExpectationsWereMet())
	mock.ExpectClose()
	require.NoError(t, db.Close())
	require.NoError(t, mock.ExpectationsWereMet())
}
