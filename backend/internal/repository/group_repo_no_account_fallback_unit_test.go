package repository

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newGroupRepoSQLite(t *testing.T) (*groupRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:group_repo_no_account_fallback?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return &groupRepository{client: client, sql: db}, client
}

// Update 走的是显式 Set/Clear 分支：漏掉 Clear 会让管理员「取消兜底」的保存
// 静默无效，字段一直保留旧值，所以往返里必须覆盖清除。
func TestGroupRepository_NoAccountFallbackRoundtrip_SQLite(t *testing.T) {
	repo, _ := newGroupRepoSQLite(t)
	ctx := context.Background()

	target := &service.Group{
		Name: "no-account-fallback-target", Platform: service.PlatformAnthropic,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1,
	}
	require.NoError(t, repo.Create(ctx, target))

	source := &service.Group{
		Name: "no-account-fallback-source", Platform: service.PlatformAnthropic,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1,
		FallbackGroupIDOnNoAccount: &target.ID,
	}
	require.NoError(t, repo.Create(ctx, source))

	got, err := repo.GetByIDLite(ctx, source.ID)
	require.NoError(t, err)
	require.NotNil(t, got.FallbackGroupIDOnNoAccount)
	require.Equal(t, target.ID, *got.FallbackGroupIDOnNoAccount)

	got.FallbackGroupIDOnNoAccount = nil
	require.NoError(t, repo.Update(ctx, got))
	cleared, err := repo.GetByIDLite(ctx, source.ID)
	require.NoError(t, err)
	require.Nil(t, cleared.FallbackGroupIDOnNoAccount)

	cleared.FallbackGroupIDOnNoAccount = &target.ID
	require.NoError(t, repo.Update(ctx, cleared))
	restored, err := repo.GetByIDLite(ctx, source.ID)
	require.NoError(t, err)
	require.NotNil(t, restored.FallbackGroupIDOnNoAccount)
	require.Equal(t, target.ID, *restored.FallbackGroupIDOnNoAccount)
}
