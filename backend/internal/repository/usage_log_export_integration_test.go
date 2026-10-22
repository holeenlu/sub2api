//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUsageExportSnapshotCursorAndLimit(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := uuid.NewString()
	user := mustCreateUser(t, client, &service.User{Email: suffix + "@export.test"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + suffix, Name: "export"})
	account := mustCreateAccount(t, client, &service.Account{Name: "export-" + suffix})
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)
	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	insert := func(id string) {
		_, err := repo.Create(ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: id, Model: "gemini-test", InputTokens: 1, CreatedAt: at})
		require.NoError(t, err)
	}
	for i := 0; i < 501; i++ {
		insert(fmt.Sprintf("%s-%d", suffix, i))
	}
	var ids []int64
	var inserted bool
	err := repo.StreamUsageLogs(ctx, usagestats.UsageLogFilters{UserID: user.ID}, 1000, func(rows []service.UsageLog) error {
		for _, row := range rows {
			require.Equal(t, user.ID, row.UserID)
			ids = append(ids, row.ID)
		}
		if !inserted {
			inserted = true
			insert(suffix + "-during-export")
		}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, ids, 501)
	for i := 1; i < len(ids); i++ {
		require.Greater(t, ids[i-1], ids[i], "equal timestamps use unique descending IDs")
	}
	require.ErrorIs(t, repo.StreamUsageLogs(ctx, usagestats.UsageLogFilters{UserID: user.ID}, 500, func([]service.UsageLog) error { return nil }), service.ErrUsageExportTooLarge)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, repo.StreamUsageLogs(canceled, usagestats.UsageLogFilters{UserID: user.ID}, 1000, func([]service.UsageLog) error { return nil }), context.Canceled)
}
func TestUsageExportRedisAcrossServices(t *testing.T) {
	ctx := context.Background()
	cache := NewLeaderLockCache(testRedis(t))
	owner := "export-owner"
	key := "usage-export:user:901"
	acquired, err := cache.TryAcquireLeaderLock(ctx, key, owner, time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	repo := newUsageLogRepositoryWithSQL(testEntClient(t), integrationDB)
	svc := service.ProvideUsageService(repo, nil, nil, nil, cache, &config.Config{})
	err = svc.ExportUsage(ctx, 901, usagestats.UsageLogFilters{UserID: 901}, func([]service.UsageLog) error { return nil })
	require.ErrorContains(t, err, "already running")
	require.NoError(t, cache.ReleaseLeaderLock(ctx, key, "stale-owner"))
	err = svc.ExportUsage(ctx, 901, usagestats.UsageLogFilters{UserID: 901}, func([]service.UsageLog) error { return nil })
	require.ErrorContains(t, err, "already running")
	require.NoError(t, cache.ReleaseLeaderLock(ctx, key, owner))
	require.NoError(t, svc.ExportUsage(ctx, 901, usagestats.UsageLogFilters{UserID: 901}, func([]service.UsageLog) error { return nil }))
}
