package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountTelemetryRuntimeOwnershipAndActivityExpiry(t *testing.T) {
	cache, mr := newStickyHistoryTestCache(t)
	ctx := t.Context()
	held, err := cache.AcquireAccountTelemetryLeader(ctx, "one")
	require.NoError(t, err)
	require.True(t, held)
	held, err = cache.AcquireAccountTelemetryLeader(ctx, "two")
	require.NoError(t, err)
	require.False(t, held)
	require.NoError(t, cache.ReleaseAccountTelemetryLeader(ctx, "two"))
	held, err = cache.AcquireAccountTelemetryLeader(ctx, "one")
	require.NoError(t, err)
	require.True(t, held)
	mr.FastForward(16 * time.Second)
	held, err = cache.AcquireAccountTelemetryLeader(ctx, "two")
	require.NoError(t, err)
	require.True(t, held)
	lease, state, err := cache.BeginAccountTelemetryRun(ctx, 42, 15*time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, lease)
	other, _, err := cache.BeginAccountTelemetryRun(ctx, 42, 15*time.Second)
	require.NoError(t, err)
	require.Empty(t, other)
	state.StartedAt = time.Now().UTC().Truncate(time.Millisecond)
	state.Initialized = true
	require.Error(t, cache.EndAccountTelemetryRun(ctx, 42, "wrong", state))
	require.NoError(t, cache.EndAccountTelemetryRun(ctx, 42, lease, state))
	lease, got, err := cache.BeginAccountTelemetryRun(ctx, 42, 15*time.Second)
	require.NoError(t, err)
	require.Equal(t, state, got)
	mr.FastForward(16 * time.Second)
	require.Error(t, cache.EndAccountTelemetryRun(ctx, 42, lease, state))
	require.NoError(t, cache.TouchAccountTelemetry(ctx, 42, service.AccountTelemetryActivity{SessionID: "wire-session"}))
	ids, err := cache.ActiveAccountTelemetry(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{42}, ids)
	activity, err := cache.GetAccountTelemetryActivity(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, "wire-session", activity.SessionID)
	mr.FastForward(10 * time.Minute)
	_, err = cache.GetAccountTelemetryActivity(ctx, 42)
	require.Error(t, err)
}

func TestAccountTelemetrySaveUsesRevisionCredentialsAndOutbox(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent_edit", true: "saved"}[changed], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := &accountRepository{client: client}
			state := service.AccountTelemetryState{Status: "pending", Revision: "new-revision", Owner: strings.Repeat("a", 64)}
			mock.ExpectBegin()
			affected := int64(0)
			if changed {
				affected = 1
			}
			mock.ExpectExec(`(?s)UPDATE accounts SET extra = .*credentials=\$3::jsonb.*revision.*=\$4.*deleted_at IS NULL`).
				WithArgs(sqlmock.AnyArg(), int64(42), "{}", "old-revision").WillReturnResult(sqlmock.NewResult(0, affected))
			if changed {
				mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(service.SchedulerOutboxEventAccountChanged, int64(42), nil, nil, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			saved, err := repo.SaveAccountTelemetry(context.Background(), 42, "{}", "old-revision", state)
			require.NoError(t, err)
			require.Equal(t, changed, saved)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
