package repository

import (
	"context"
	"database/sql/driver"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestInsertSystemMetricsNullableIntegerMetrics(t *testing.T) {
	createdAt := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
	zero := 0

	tests := []struct {
		name         string
		dbConnActive *int
		wantDBActive driver.Value
	}{
		{name: "explicit zero is preserved", dbConnActive: &zero, wantDBActive: int64(0)},
		{name: "unavailable metric remains null", dbConnActive: nil, wantDBActive: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })

			args := make([]driver.Value, 40)
			args[0] = createdAt
			args[1] = int64(1)
			for i := 4; i <= 12; i++ {
				args[i] = int64(0)
			}
			args[35] = tt.wantDBActive

			mock.ExpectExec("INSERT INTO ops_system_metrics").
				WithArgs(args...).
				WillReturnResult(sqlmock.NewResult(1, 1))

			repo := &opsRepository{db: db}
			err = repo.InsertSystemMetrics(context.Background(), &service.OpsInsertSystemMetricsInput{
				CreatedAt:    createdAt,
				DBConnActive: tt.dbConnActive,
			})
			if err != nil {
				t.Fatalf("InsertSystemMetrics: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}

func TestListJobHeartbeatsLoadsExpectedInterval(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT[\\s\\S]*COALESCE\\(expected_interval_seconds, 0\\)[\\s\\S]*FROM ops_job_heartbeats").WillReturnRows(sqlmock.NewRows([]string{"job_name", "last_run_at", "last_success_at", "last_error_at", "last_error", "last_duration_ms", "last_result", "updated_at", "expected_interval_seconds"}).AddRow("ops_cleanup", now, now, nil, nil, 20, "ok", now, 86400))
	jobs, err := (&opsRepository{db: db}).ListJobHeartbeats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ExpectedIntervalSeconds != 86400 {
		t.Fatalf("daily interval not loaded: %+v", jobs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// The SQL mock cannot detect PostgreSQL parameter inference (varchar vs text).
// Exercise the actual repository statement and NULL-safe duplicate check.
func TestInsertSystemMetricsPostgresParameterTypes(t *testing.T) {
	_, db := vendorSupplyPostgres(t)
	migration, err := os.ReadFile("../../migrations/033_ops_monitoring_vnext.sql")
	require.NoError(t, err)
	start := strings.Index(string(migration), "CREATE TABLE IF NOT EXISTS ops_system_metrics (")
	end := strings.Index(string(migration)[start:], "\n);") + start + 3
	_, err = db.Exec(string(migration)[start:end])
	require.NoError(t, err)
	_, err = db.Exec(`ALTER TABLE ops_system_metrics ADD COLUMN account_switch_count BIGINT NOT NULL DEFAULT 0, ADD COLUMN redis_conn_total INT, ADD COLUMN redis_conn_idle INT`)
	require.NoError(t, err)
	repo := &opsRepository{db: db}
	platform, group := "openai", int64(7)
	now := time.Now().UTC().Truncate(time.Minute)
	for _, input := range []*service.OpsInsertSystemMetricsInput{
		{CreatedAt: now, SuccessCount: 3},
		{CreatedAt: now, SuccessCount: 999},
		{CreatedAt: now, Platform: &platform, GroupID: &group, SuccessCount: 4},
		{CreatedAt: now, Platform: &platform, GroupID: &group, SuccessCount: 999},
		{CreatedAt: now.Add(time.Minute), Platform: &platform, GroupID: &group, SuccessCount: 5},
	} {
		require.NoError(t, repo.InsertSystemMetrics(t.Context(), input))
	}
	var count, sum int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*), SUM(success_count) FROM ops_system_metrics`).Scan(&count, &sum))
	require.Equal(t, 3, count)
	require.Equal(t, 12, sum)
}
