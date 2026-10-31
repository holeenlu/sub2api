//go:build unit

package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func opsCoverageHourlyRows(buckets ...time.Time) *sqlmock.Rows {
	columns := make([]string, 22)
	for i := range columns {
		columns[i] = fmt.Sprint(i)
	}
	rows := sqlmock.NewRows(columns)
	for _, bucket := range buckets {
		values := make([]driver.Value, len(columns))
		for i := range values {
			values[i] = int64(0)
		}
		values[0], values[1] = bucket, int64(1)
		rows.AddRow(values...)
	}
	return rows
}

func TestOpsPreaggCoverageDistinguishesEmptyMissingAndUnavailable(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	unavailable := errors.New("coverage dependency unavailable")
	for _, tc := range []struct {
		name        string
		rows        []opsHourlyMetricsRow
		usageExists bool
		errorExists bool
		usageError  error
		errorError  error
		wantError   error
	}{
		{name: "empty window"},
		{name: "empty aggregation but usage", usageExists: true, wantError: service.ErrOpsPreaggregatedNotPopulated},
		{name: "partial aggregation with usage hole", rows: []opsHourlyMetricsRow{{bucketStart: start}}, usageExists: true, wantError: service.ErrOpsPreaggregatedNotPopulated},
		{name: "partial aggregation with error hole", rows: []opsHourlyMetricsRow{{bucketStart: start}}, errorExists: true, wantError: service.ErrOpsPreaggregatedNotPopulated},
		{name: "legitimate empty hours", rows: []opsHourlyMetricsRow{{bucketStart: start}}},
		{name: "usage probe unavailable", usageError: unavailable, wantError: unavailable},
		{name: "error probe unavailable", errorError: unavailable, wantError: unavailable},
		{name: "deadline", usageError: context.DeadlineExceeded, wantError: context.DeadlineExceeded},
		{name: "canceled", usageError: context.Canceled, wantError: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			db.SetMaxOpenConns(1)
			usage := mock.ExpectQuery("SELECT EXISTS.*FROM usage_logs")
			if tc.usageError != nil {
				usage.WillReturnError(tc.usageError)
			} else {
				usage.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tc.usageExists))
			}
			if tc.usageError == nil && !tc.usageExists {
				errorsQuery := mock.ExpectQuery("SELECT EXISTS.*FROM ops_error_logs")
				if tc.errorError != nil {
					errorsQuery.WillReturnError(tc.errorError)
				} else {
					errorsQuery.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tc.errorExists))
				}
			}
			err := (&opsRepository{db: db}).validateHourlyCoverage(t.Context(), nil, start, end, tc.rows)
			if tc.wantError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantError)
			}
			require.NoError(t, mock.ExpectationsWereMet())
			require.Zero(t, db.Stats().InUse)
		})
	}
}

func TestOpsPreaggCoverageUsesDistinctResultBucketsAndBoundScope(t *testing.T) {
	db, mock := newSQLMock(t)
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	groupID := int64(42)
	filter := &service.OpsDashboardFilter{Platform: " OpenAI ", GroupID: &groupID}
	// A duplicate and a bucket outside the requested range cannot mask a hole.
	rows := []opsHourlyMetricsRow{{bucketStart: start}, {bucketStart: start}, {bucketStart: end}}
	covered := pq.Array([]string{start.Format(time.RFC3339)})
	mock.ExpectQuery("SELECT EXISTS.*FROM usage_logs.*"+regexp.QuoteMeta("ANY($5::timestamptz[])")).
		WithArgs(start, end, groupID, "openai", covered).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS.*FROM ops_error_logs.*is_count_tokens = FALSE.*"+regexp.QuoteMeta("ANY($5::timestamptz[])")).
		WithArgs(start, end, groupID, "openai", covered).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	err := (&opsRepository{db: db}).validateHourlyCoverage(t.Context(), filter, start, end, rows)
	require.ErrorIs(t, err, service.ErrOpsPreaggregatedNotPopulated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsPreaggCoverageCompleteHoursNeedNoRawProbe(t *testing.T) {
	db, mock := newSQLMock(t)
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []opsHourlyMetricsRow{{bucketStart: start.Add(time.Hour)}, {bucketStart: start}, {bucketStart: start}}
	require.NoError(t, (&opsRepository{db: db}).validateHourlyCoverage(t.Context(), nil, start, start.Add(2*time.Hour), rows))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsPreaggCoveragePublicOverviewPreservesModeAndDependencyErrors(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	unavailable := errors.New("raw coverage unavailable")
	for _, mode := range []service.OpsQueryMode{service.OpsQueryModePreagg, service.OpsQueryModeAuto} {
		for _, probeError := range []error{nil, unavailable} {
			t.Run(fmt.Sprintf("%s/error_%t", mode, probeError != nil), func(t *testing.T) {
				db, mock := newSQLMock(t)
				db.SetMaxOpenConns(1)
				mock.ExpectQuery("FROM ops_metrics_hourly").WillReturnRows(opsCoverageHourlyRows(start)).RowsWillBeClosed()
				probe := mock.ExpectQuery("SELECT EXISTS.*FROM usage_logs")
				if probeError != nil {
					probe.WillReturnError(probeError)
				} else {
					probe.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				}
				if mode == service.OpsQueryModeAuto && probeError == nil {
					// Existing raw fallback: six overview queries followed by output TPS.
					for index, count := range []int{2, 13, 6, 2, 6, 2, 5} {
						columns, values := make([]string, count), make([]driver.Value, count)
						for i := range columns {
							columns[i], values[i] = fmt.Sprint(i), int64(0)
						}
						if index == 0 {
							values[0] = int64(7)
						}
						mock.ExpectQuery("SELECT|WITH").WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
					}
				}
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				out, err := NewOpsRepository(db).GetDashboardOverview(ctx, &service.OpsDashboardFilter{StartTime: start, EndTime: end, QueryMode: mode})
				switch {
				case probeError != nil:
					require.ErrorIs(t, err, probeError)
					require.Nil(t, out, "dependency failure must never look like an empty/successful window")
				case mode == service.OpsQueryModePreagg:
					require.ErrorIs(t, err, service.ErrOpsPreaggregatedNotPopulated)
					require.Nil(t, out)
				default:
					require.NoError(t, err)
					require.EqualValues(t, 7, out.SuccessCount, "auto must use raw counts, not the one partial aggregate row")
				}
				require.NoError(t, mock.ExpectationsWereMet())
				require.Zero(t, db.Stats().InUse)
			})
		}
	}
}
