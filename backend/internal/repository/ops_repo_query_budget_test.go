package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type opsBudgetConnector struct {
	query    func(context.Context, string) (driver.Rows, error)
	exec     func(context.Context, string) (driver.Result, error)
	rollback func() error
}

func (c *opsBudgetConnector) Connect(context.Context) (driver.Conn, error) {
	return &opsBudgetConn{owner: c}, nil
}
func (c *opsBudgetConnector) Driver() driver.Driver { return opsBudgetDriver{} }

type opsBudgetDriver struct{}

func (opsBudgetDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type opsBudgetConn struct{ owner *opsBudgetConnector }

func (*opsBudgetConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*opsBudgetConn) Close() error { return nil }
func (c *opsBudgetConn) Begin() (driver.Tx, error) {
	return &opsBudgetTx{rollback: c.owner.rollback}, nil
}
func (c *opsBudgetConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *opsBudgetConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.owner.query != nil {
		return c.owner.query(ctx, query)
	}
	return &opsBudgetRows{}, nil
}
func (c *opsBudgetConn) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if c.owner.exec != nil {
		return c.owner.exec(ctx, query)
	}
	return driver.RowsAffected(1), nil
}

type opsBudgetRows struct {
	close func() error
	read  bool
}

func (*opsBudgetRows) Columns() []string { return []string{"value"} }
func (r *opsBudgetRows) Close() error {
	if r.close != nil {
		return r.close()
	}
	return nil
}
func (r *opsBudgetRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	dest[0] = int64(1)
	return nil
}

type opsBudgetTx struct{ rollback func() error }

func (*opsBudgetTx) Commit() error { return nil }
func (t *opsBudgetTx) Rollback() error {
	if t.rollback != nil {
		return t.rollback()
	}
	return nil
}

func newOpsBudgetDB(t *testing.T, maxOpen int, connector *opsBudgetConnector) *sql.DB {
	t.Helper()
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(maxOpen)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpsQueryBudgetEntryPointsShareCapacity(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	filter := &service.OpsDashboardFilter{StartTime: start, EndTime: end}
	for _, constructor := range []bool{false, true} {
		t.Run(fmt.Sprintf("constructor_%t", constructor), func(t *testing.T) {
			var queries atomic.Int32
			db := newOpsBudgetDB(t, 3, &opsBudgetConnector{query: func(context.Context, string) (driver.Rows, error) {
				queries.Add(1)
				return &opsBudgetRows{}, nil
			}})
			repo := &opsRepository{db: db}
			if constructor {
				repo = NewOpsRepository(db).(*opsRepository)
			}
			held, err := repo.queryOpsRows(t.Context(), "hold observation connection")
			require.NoError(t, err)
			defer held.Close()
			for name, call := range map[string]func(context.Context) error{
				"errors":          func(ctx context.Context) error { _, err := repo.ListErrorLogs(ctx, nil); return err },
				"ingress rejects": func(ctx context.Context) error { _, err := repo.ListIngressRejects(ctx, nil); return err },
				"error detail":    func(ctx context.Context) error { _, err := repo.GetErrorLogByID(ctx, 1); return err },
				"system logs":     func(ctx context.Context) error { _, err := repo.ListSystemLogs(ctx, nil); return err },
				"request export":  func(ctx context.Context) error { _, _, err := repo.ListRequestDetails(ctx, nil); return err },
				"overview":        func(ctx context.Context) error { _, err := repo.GetDashboardOverview(ctx, filter); return err },
				"window":          func(ctx context.Context) error { _, err := repo.GetWindowStats(ctx, filter); return err },
				"throughput":      func(ctx context.Context) error { _, err := repo.GetThroughputTrend(ctx, filter, 60); return err },
				"error trend":     func(ctx context.Context) error { _, err := repo.GetErrorTrend(ctx, filter, 60); return err },
				"distribution":    func(ctx context.Context) error { _, err := repo.GetErrorDistribution(ctx, filter); return err },
				"histogram":       func(ctx context.Context) error { _, err := repo.GetLatencyHistogram(ctx, filter); return err },
				"realtime":        func(ctx context.Context) error { _, err := repo.GetRealtimeTrafficSummary(ctx, filter); return err },
				"token stats": func(ctx context.Context) error {
					_, err := repo.GetOpenAITokenStats(ctx, &service.OpsOpenAITokenStatsFilter{StartTime: start, EndTime: end})
					return err
				},
				"collector window": func(ctx context.Context) error { _, err := repo.GetSystemMetricsWindow(ctx, start, end); return err },
				"retention batch": func(ctx context.Context) error {
					_, err := repo.CleanupOpsTable(ctx, "ops_error_logs", start, false, 5000)
					return err
				},
				"retention truncate": func(ctx context.Context) error {
					_, err := repo.CleanupOpsTable(ctx, "ops_error_logs", start, true, 5000)
					return err
				},
				"hourly aggregate": func(ctx context.Context) error { return repo.UpsertHourlyMetrics(ctx, start, end) },
				"daily aggregate":  func(ctx context.Context) error { return repo.UpsertDailyMetrics(ctx, start, end) },
				"hourly watermark": func(ctx context.Context) error { _, _, err := repo.GetLatestHourlyBucketStart(ctx); return err },
				"daily watermark":  func(ctx context.Context) error { _, _, err := repo.GetLatestDailyBucketDate(ctx); return err },
				"alert events":     func(ctx context.Context) error { _, err := repo.ListAlertEvents(ctx, nil); return err },
				"filtered cleanup": func(ctx context.Context) error {
					_, err := repo.DeleteSystemLogs(ctx, &service.OpsSystemLogCleanupFilter{RequestID: "request"})
					return err
				},
			} {
				t.Run(name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
					defer cancel()
					require.ErrorIs(t, call(ctx), context.DeadlineExceeded)
					require.Equal(t, int32(1), queries.Load(), "queued observation must not occupy a second SQL connection")
					require.Equal(t, 1, db.Stats().InUse)
				})
			}
			// 轻量写入不排在观测查询后面，且使用业务池中保留的连接。
			require.NoError(t, repo.UpsertJobHeartbeat(t.Context(), &service.OpsUpsertJobHeartbeatInput{JobName: "heartbeat"}))
		})
	}
}

func TestOpsQueryBudgetBoundsActualConnectionsAndPreservesBusinessCapacity(t *testing.T) {
	for _, tc := range []struct{ maxOpen, capacity int }{{0, 1}, {1, 1}, {3, 1}, {5, 3}, {20, 4}} {
		t.Run(fmt.Sprint(tc.maxOpen), func(t *testing.T) {
			db := newOpsBudgetDB(t, tc.maxOpen, &opsBudgetConnector{})
			repo := &opsRepository{db: db}
			var held []*opsQueryRows
			defer func() {
				for _, rows := range held {
					_ = rows.Close()
				}
			}()
			for range tc.capacity {
				rows, err := repo.queryOpsRows(t.Context(), "held query")
				require.NoError(t, err)
				held = append(held, rows)
			}
			require.Equal(t, tc.capacity, db.Stats().InUse)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			_, err := repo.execOpsQuery(ctx, "queued aggregation")
			require.ErrorIs(t, err, context.DeadlineExceeded)
			canceledCtx, cancelWaiter := context.WithCancel(t.Context())
			canceled := make(chan error, 1)
			go func() { _, err := repo.execOpsQuery(canceledCtx, "canceled aggregation"); canceled <- err }()
			cancelWaiter()
			require.ErrorIs(t, <-canceled, context.Canceled)
			if tc.maxOpen != 1 {
				_, err := db.ExecContext(t.Context(), "business request")
				require.NoError(t, err)
			}
			require.NoError(t, held[0].Close())
			_, err = repo.execOpsQuery(t.Context(), "next aggregation")
			require.NoError(t, err)
			for _, rows := range held {
				require.NoError(t, rows.Close())
			}
			require.Zero(t, db.Stats().InUse)
		})
	}
}

func TestOpsQueryBudgetHoldsPermitThroughRowsCloseAndCancellation(t *testing.T) {
	for _, cancelRead := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel_%t", cancelRead), func(t *testing.T) {
			closing, finishClose := make(chan struct{}), make(chan struct{})
			finish := sync.OnceFunc(func() { close(finishClose) })
			db := newOpsBudgetDB(t, 3, &opsBudgetConnector{query: func(_ context.Context, query string) (driver.Rows, error) {
				rows := &opsBudgetRows{}
				if query == "slow close" {
					rows.close = func() error { close(closing); <-finishClose; return nil }
				}
				return rows, nil
			}})
			t.Cleanup(finish)
			repo := &opsRepository{db: db}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			held, err := repo.queryOpsRows(ctx, "slow close")
			require.NoError(t, err)
			if cancelRead {
				cancel()
			}
			closed := make(chan error, 1)
			go func() { closed <- held.Close() }()
			<-closing
			waitCtx, stopWait := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer stopWait()
			_, err = repo.execOpsQuery(waitCtx, "must wait for rows cleanup")
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, 1, db.Stats().InUse)
			finish()
			require.NoError(t, <-closed)
			var value int
			require.NoError(t, repo.queryOpsRow(t.Context(), "after cleanup").Scan(&value))
			require.Equal(t, 1, value)
			require.Zero(t, db.Stats().InUse)
		})
	}
}

func TestOpsQueryBudgetHoldsPermitThroughTransactionRollback(t *testing.T) {
	rollingBack, finishRollback := make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(finishRollback) })
	db := newOpsBudgetDB(t, 3, &opsBudgetConnector{rollback: func() error {
		close(rollingBack)
		<-finishRollback
		return nil
	}})
	t.Cleanup(finish)
	repo := &opsRepository{db: db}
	lease, err := repo.acquireOpsQuery(t.Context())
	require.NoError(t, err)
	tx, err := lease.conn.BeginTx(lease.ctx, nil)
	require.NoError(t, err)
	rolledBack := make(chan error, 1)
	go func() { rolledBack <- tx.Rollback() }()
	<-rollingBack
	closed := make(chan struct{})
	go func() { lease.close(); close(closed) }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = repo.execOpsQuery(ctx, "must wait for rollback")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, db.Stats().InUse)
	finish()
	require.NoError(t, <-rolledBack)
	<-closed
	_, err = repo.execOpsQuery(t.Context(), "after rollback")
	require.NoError(t, err)
	require.Zero(t, db.Stats().InUse)
}

func TestOpsQueryBudgetReleasesBeforeThroughputDrilldown(t *testing.T) {
	for _, maxOpen := range []int{1, 3} {
		t.Run(fmt.Sprint(maxOpen), func(t *testing.T) {
			db, mock := newSQLMock(t)
			db.SetMaxOpenConns(maxOpen)
			mock.ExpectQuery("WITH usage_buckets").WillReturnRows(sqlmock.NewRows([]string{"bucket", "requests", "tokens", "switches"})).RowsWillBeClosed()
			mock.ExpectQuery("WITH usage_totals").WillReturnRows(sqlmock.NewRows([]string{"platform", "requests", "tokens"})).RowsWillBeClosed()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			_, err := (&opsRepository{db: db}).GetThroughputTrend(ctx, &service.OpsDashboardFilter{StartTime: start, EndTime: start.Add(time.Hour)}, 60)
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
			require.Zero(t, db.Stats().InUse)
		})
	}
}
func TestOpsQueryBudgetDeadlineAndFailureRelease(t *testing.T) {
	queryFailure := errors.New("query failed")
	var observedDeadline time.Time
	db := newOpsBudgetDB(t, 3, &opsBudgetConnector{query: func(ctx context.Context, query string) (driver.Rows, error) {
		observedDeadline, _ = ctx.Deadline()
		if query == "failure" {
			return nil, queryFailure
		}
		return &opsBudgetRows{}, nil
	}})
	repo := &opsRepository{db: db}
	_, err := repo.queryOpsRows(context.Background(), "failure")
	require.ErrorIs(t, err, queryFailure)
	require.True(t, observedDeadline.After(time.Now()))
	require.LessOrEqual(t, time.Until(observedDeadline), 30*time.Second)
	require.Zero(t, db.Stats().InUse)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	var value int
	require.NoError(t, repo.queryOpsRow(ctx, "existing deadline").Scan(&value))
	require.Equal(t, deadline, observedDeadline)
	require.Zero(t, db.Stats().InUse)
}
