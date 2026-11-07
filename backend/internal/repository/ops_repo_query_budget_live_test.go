//go:build unit

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This test must be explicitly pointed at its disposable loopback database.
// It never uses application configuration, migrations or a production schema.
func TestOpsQueryBudgetRealPostgresCancellationAndPoolReuse(t *testing.T) {
	dsn := os.Getenv("CPR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated CPR_TEST_POSTGRES_DSN")
	}
	target, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", target.Hostname())
	require.Equal(t, "/cpr_review", target.Path)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	db.SetMaxOpenConns(3) // one analytical query, two slots remain for business.
	repo := &opsRepository{db: db}
	require.NoError(t, db.PingContext(t.Context()))

	held, err := repo.queryOpsRows(t.Context(), "SELECT generate_series(1, 5)")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, held.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	_, err = repo.queryOpsRows(ctx, "SELECT 1")
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var value int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT 7").Scan(&value))
	require.Equal(t, 7, value, "business connection remains available")
	require.NoError(t, held.Close())

	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	_, err = repo.execOpsQuery(ctx, "SELECT pg_sleep(5)")
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded, "lib/pq cancellation retains the caller cause")
	require.NoError(t, repo.queryOpsRow(t.Context(), "SELECT 11").Scan(&value))
	require.Equal(t, 11, value)
	require.Zero(t, db.Stats().InUse, "the lease ends only after the SQL connection returns")
	repo.queryBudget.mu.Lock()
	active := repo.queryBudget.active
	repo.queryBudget.mu.Unlock()
	require.Zero(t, active)
}
