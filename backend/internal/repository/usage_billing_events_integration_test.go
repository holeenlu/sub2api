//go:build integration

package repository

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestUsageBillingRecoveryCrashAndDuplicate(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Balance: 100})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "recovery"})
	account := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString()})
	repo := &usageBillingRepository{db: integrationDB}
	id := uuid.NewString()
	cmd := &service.UsageBillingCommand{RequestID: id, APIKeyID: key.ID, UserID: user.ID, AccountID: account.ID, BalanceCost: 1.25, Platform: "anthropic", UsageLog: &service.UsageLog{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, Model: "test", ActualCost: 1.25, TotalCost: 1.25, CreatedAt: time.Now()}}
	limit := 100.0
	quota := NewUserPlatformQuotaRepository(client)
	require.NoError(t, quota.BulkInsertInitial(ctx, []UserPlatformQuotaRecord{{UserID: user.ID, Platform: "anthropic", DailyLimitUSD: &limit}}))
	// Persist then lose the submitting process before the debit.
	require.NoError(t, repo.persistUsageBillingEvent(ctx, cmd))
	// A claimed event is unavailable until its lease expires, even in a fresh worker.
	_, err := integrationDB.ExecContext(ctx, "UPDATE usage_billing_events SET available_at=NOW()+INTERVAL '30 seconds' WHERE request_id=$1 AND api_key_id=$2", id, key.ID)
	require.NoError(t, err)
	_, err = repo.RecoverPendingUsageBilling(ctx, 64, nil)
	require.NoError(t, err)
	var before float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&before))
	require.Equal(t, 100.0, before)
	_, err = integrationDB.ExecContext(ctx, "UPDATE usage_billing_events SET available_at=NOW()-INTERVAL '1 second' WHERE request_id=$1 AND api_key_id=$2", id, key.ID)
	require.NoError(t, err)
	repo.invalidate = func(context.Context, *service.UsageBillingCommand) error {
		return errors.New("redis temporarily unavailable")
	}
	count, err := repo.RecoverPendingUsageBilling(ctx, 64, nil)
	require.NoError(t, err)
	require.Positive(t, count)
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2", id, key.ID).Scan(&status))
	require.Equal(t, "settled", status)
	_, err = integrationDB.ExecContext(ctx, "UPDATE usage_billing_events SET available_at=NOW() WHERE request_id=$1 AND api_key_id=$2", id, key.ID)
	require.NoError(t, err)
	repo.invalidate = func(context.Context, *service.UsageBillingCommand) error { return nil }
	_, err = repo.RecoverPendingUsageBilling(ctx, 64, nil)
	require.NoError(t, err)
	_, err = repo.Apply(ctx, cmd)
	require.NoError(t, err)
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.Equal(t, 98.75, balance)
	q, err := quota.GetByUserPlatform(ctx, user.ID, "anthropic")
	require.NoError(t, err)
	require.Equal(t, 1.25, q.DailyUsageUSD)
	var rows int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2 AND billing_status='settled'", id, key.ID).Scan(&rows))
	require.Equal(t, 1, rows)
	// Admin reset wins; retrying this completed settlement cannot restore a stale snapshot.
	require.NoError(t, quota.ResetExpiredWindow(ctx, user.ID, "anthropic", "daily", time.Now()))
	_, err = repo.Apply(ctx, cmd)
	require.NoError(t, err)
	q, err = quota.GetByUserPlatform(ctx, user.ID, "anthropic")
	require.NoError(t, err)
	require.Zero(t, q.DailyUsageUSD)
}
func TestUsageCursorTiesAndChangedFilter(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "cursor"})
	account := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString()})
	repo := NewUsageLogRepository(client, integrationDB)
	at := time.Now().Truncate(time.Microsecond)
	for i := 0; i < 5; i++ {
		_, err := repo.Create(ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "same", RequestedModel: "same", CreatedAt: at})
		require.NoError(t, err)
	}
	for _, sort := range []string{"created_at", "model", "id"} {
		for _, order := range []string{"asc", "desc"} {
			cursor := "start"
			seen := map[int64]bool{}
			filters := usagestats.UsageLogFilters{UserID: user.ID}
			for page := 1; page < 5; page++ {
				logs, p, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: page, PageSize: 2, Cursor: cursor, SortBy: sort, SortOrder: order}, filters)
				require.NoError(t, err)
				require.False(t, p.TotalExact, "without explicit COUNT the total remains observational")
				for _, log := range logs {
					require.False(t, seen[log.ID])
					seen[log.ID] = true
				}
				if !p.HasMore {
					break
				}
				cursor = p.NextCursor
			}
			require.Len(t, seen, 5)
			_, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{PageSize: 2, Cursor: cursor, SortBy: sort, SortOrder: order}, usagestats.UsageLogFilters{UserID: user.ID, Model: "different"})
			require.ErrorIs(t, err, errUsageCursorInvalid)
		}
	}
}

func TestUsageBillingRecoveryFreezesRepairedPrice(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Balance: 100})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "pricing"})
	account := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString()})
	id := uuid.NewString()
	repo := &usageBillingRepository{db: integrationDB}
	cmd := &service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, Pricing: &service.UsageBillingPricingSnapshot{Pending: true}, UsageLog: &service.UsageLog{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, Model: "repairable", CreatedAt: time.Now(), BillingStatus: "pending_pricing"}}
	_, err := repo.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrUsagePricingPending)
	calls := 0
	repo.reprice = func(_ context.Context, c *service.UsageBillingCommand) error {
		calls++
		c.Pricing.Pending = false
		c.BalanceCost = 2
		c.UsageLog.ActualCost = 2
		c.UsageLog.TotalCost = 2
		return nil
	}
	repo.invalidate = func(context.Context, *service.UsageBillingCommand) error { return errors.New("temporary cache error") }
	_, err = repo.RecoverPendingUsageBilling(ctx, 64, nil)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	_, err = integrationDB.ExecContext(ctx, "UPDATE usage_billing_events SET available_at=NOW() WHERE request_id=$1 AND api_key_id=$2", id, key.ID)
	require.NoError(t, err)
	repo.invalidate = func(context.Context, *service.UsageBillingCommand) error { return nil }
	_, err = repo.RecoverPendingUsageBilling(ctx, 64, nil)
	require.NoError(t, err)
	require.Equal(t, 1, calls, "a settled price must not be evaluated again")
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.Equal(t, 98.0, balance)
}
