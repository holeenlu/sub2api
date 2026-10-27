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
		_, err := repo.Create(ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: id, Model: "gpt-test", InputTokens: 1, CacheCreationTokens: 34, CacheCreation30mTokens: 13, CacheCreation12hTokens: 21, CreatedAt: at})
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
			require.Equal(t, 34, row.CacheCreationTokens)
			require.Equal(t, 13, row.CacheCreation30mTokens)
			require.Equal(t, 21, row.CacheCreation12hTokens)
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

func TestUsageSamplingDetailsPersistsWithOneLedgerAndExports(t *testing.T) {
	ctx := t.Context()
	client := testEntClient(t)
	suffix := uuid.NewString()
	user := mustCreateUser(t, client, &service.User{Balance: 10})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: suffix, Name: "sample-detail"})
	account := mustCreateAccount(t, client, &service.Account{Name: suffix})
	cost := 1.25
	details := &service.UsageSamplingDetails{Version: 1, Scope: "cloud_turn", PricingMode: "token", Samples: []service.UsageSamplingSegment{{InputTokens: 10, CacheReadTokens: 90, OutputTokens: 2, TotalCost: &cost, ActualCost: &cost}}}
	log := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: suffix, Model: "gpt-test", InputTokens: 10, CacheReadTokens: 90, OutputTokens: 2, TotalCost: cost, ActualCost: cost, SamplingDetails: details, CreatedAt: time.Now()}
	repo := &usageBillingRepository{db: integrationDB}
	cmd := &service.UsageBillingCommand{RequestID: suffix, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, BalanceCost: cost, UsageLog: log}
	for range 2 {
		_, err := repo.applyUsageBillingTransaction(ctx, cmd)
		require.NoError(t, err)
	}
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.InDelta(t, 8.75, balance, 1e-9)
	logs := newUsageLogRepositoryWithSQL(client, integrationDB)
	count := 0
	require.NoError(t, logs.StreamUsageLogs(ctx, usagestats.UsageLogFilters{UserID: user.ID}, 10, func(rows []service.UsageLog) error {
		for _, row := range rows {
			count++
			require.Equal(t, details, row.SamplingDetails)
		}
		return nil
	}))
	require.Equal(t, 1, count, "breakdown is not additional usage rows or debits")
}
