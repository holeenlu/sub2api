package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpsRepositoryCleanupPreservesBatchColumnsAndTruncateCounts(t *testing.T) {
	db, mock := newSQLMock(t)
	db.SetMaxOpenConns(1)
	repo := &opsRepository{db: db}
	cutoff := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ table, predicate string }{
		{"ops_error_logs", "created_at < \\$1"},
		{"ops_ingress_reject_aggregates", "bucket_start < \\$1"},
		{"ops_metrics_daily", "bucket_date < \\$1::date"},
	} {
		mock.ExpectExec("(?s)SELECT id FROM "+tc.table+".*WHERE "+tc.predicate+".*ORDER BY id.*LIMIT \\$2.*DELETE FROM "+tc.table).
			WithArgs(cutoff, 5000).WillReturnResult(sqlmock.NewResult(0, 17))
		count, err := repo.CleanupOpsTable(t.Context(), tc.table, cutoff, false, 5000)
		require.NoError(t, err)
		require.Equal(t, int64(17), count)
	}
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM ops_error_logs$").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(27))
	mock.ExpectExec("TRUNCATE TABLE ops_error_logs$").WillReturnResult(sqlmock.NewResult(0, 0))
	count, err := repo.CleanupOpsTable(t.Context(), "ops_error_logs", time.Time{}, true, 5000)
	require.NoError(t, err)
	require.Equal(t, int64(27), count)
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM ops_error_logs$").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	count, err = repo.CleanupOpsTable(t.Context(), "ops_error_logs", time.Time{}, true, 5000)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Zero(t, db.Stats().InUse)
}

func TestOpsRepositoryCleanupKeepsMissingRelationAndFailureSemantics(t *testing.T) {
	missing := errors.New(`pq: relation "ops_error_logs" does not exist`)
	failure := errors.New("connection refused")
	for _, cause := range []error{missing, failure} {
		for _, stage := range []string{"delete", "count", "truncate"} {
			t.Run(stage+"/"+cause.Error(), func(t *testing.T) {
				db, mock := newSQLMock(t)
				repo := &opsRepository{db: db}
				if stage == "delete" {
					mock.ExpectExec("DELETE FROM ops_error_logs").WillReturnError(cause)
				} else if stage == "count" {
					mock.ExpectQuery("SELECT COUNT").WillReturnError(cause)
				} else {
					mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
					mock.ExpectExec("TRUNCATE TABLE").WillReturnError(cause)
				}
				count, err := repo.CleanupOpsTable(t.Context(), "ops_error_logs", time.Now(), stage != "delete", 5000)
				if cause == missing {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, cause)
				}
				require.Zero(t, count)
				require.NoError(t, mock.ExpectationsWereMet())
				require.Zero(t, db.Stats().InUse)
			})
		}
	}
}

func TestOpsRepositoryCleanupCancellationAfterCountDoesNotTruncate(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writes := 0
	db := newOpsBudgetDB(t, 1, &opsBudgetConnector{
		query: func(context.Context, string) (driver.Rows, error) {
			return &opsBudgetRows{close: func() error { cancel(); return nil }}, nil
		},
		exec: func(context.Context, string) (driver.Result, error) { writes++; return driver.RowsAffected(1), nil },
	})
	count, err := (&opsRepository{db: db}).CleanupOpsTable(ctx, "ops_error_logs", time.Time{}, true, 5000)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, count)
	require.Zero(t, writes)
	require.Zero(t, db.Stats().InUse)
}

func TestIsMissingRelationError(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New(`pq: relation "ops_error_logs" does not exist`), true},
		{errors.New(`ERROR: Relation "x" Does Not Exist`), true},
		{errors.New("connection refused"), false},
	} {
		require.Equal(t, tc.want, isMissingRelationError(tc.err))
	}
}
