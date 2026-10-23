//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type failingTransferLedger struct {
	service.RedeemCodeRepository
	calls int
}

func (r *failingTransferLedger) Create(ctx context.Context, entry *service.RedeemCode) error {
	r.calls++
	if r.calls == 2 {
		return errors.New("second ledger write failed")
	}
	return r.RedeemCodeRepository.Create(ctx, entry)
}

func TestAdminFundingOwnershipAndAtomicTransfers(t *testing.T) {
	repo, settings, root, admin := rbacFixture(t)
	client := testEntClient(t)
	admin.Permissions = append(admin.Permissions, "billing.balance.adjust")
	adminCtx := authz.WithSubject(context.Background(), admin)
	rootCtx := authz.WithSubject(context.Background(), root)
	_, err := integrationDB.Exec("UPDATE users SET balance=5000 WHERE id=$1", admin.UserID)
	require.NoError(t, err)
	ledger := NewRedeemCodeRepository(client)
	svc := service.NewAdminService(nil, repo, nil, nil, nil, nil, ledger, nil, nil, nil, nil, nil, nil, client, settings, nil, nil, nil, nil, nil, nil, nil, nil)
	suffix := time.Now().UnixNano()
	create := func(ctx context.Context, name, role string, amount float64) (*service.User, error) {
		return svc.CreateUser(ctx, &service.CreateUserInput{Email: fmt.Sprintf("%s-%d@example.com", name, suffix), Password: "safe-test-password", Role: role, Balance: &amount})
	}
	// Even a stale legacy staff.manage grant cannot create or promote an admin.
	_, err = create(adminCtx, "forbidden", authz.Admin, 0)
	require.Error(t, err)
	own, err := create(adminCtx, "owned", authz.User, 0)
	require.NoError(t, err)
	foreign, err := create(rootCtx, "foreign", authz.User, 0)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM redeem_codes WHERE used_by IN($1,$2)", own.ID, admin.UserID)
		_, _ = integrationDB.Exec("DELETE FROM users WHERE id IN($1,$2)", own.ID, foreign.ID)
	})
	require.NotNil(t, own.CreatedBy)
	require.Equal(t, admin.UserID, *own.CreatedBy)
	_, err = svc.GetUser(adminCtx, foreign.ID)
	require.Error(t, err)
	_, _, _, err = svc.GetUserBalanceHistory(adminCtx, foreign.ID, 1, 20, "")
	require.Error(t, err)
	_, err = svc.UpdateUser(adminCtx, own.ID, &service.UpdateUserInput{Role: authz.Admin})
	require.Error(t, err)
	_, err = svc.UpdateUserBalance(adminCtx, foreign.ID, 1, "add", "")
	require.Error(t, err)
	users, total, err := repo.ListWithFilters(adminCtx, pagination.PaginationParams{Page: 1, PageSize: 100}, service.UserListFilters{})
	require.NoError(t, err)
	require.EqualValues(t, 1, total.Total)
	require.Len(t, users, 1)
	require.Equal(t, own.ID, users[0].ID)
	// Batch dashboards must not expose another administrator's customer spending.
	account := client.Account.Create().SetName("funding-scope").SetPlatform("openai").SetType("apikey").SetCredentials(map[string]any{}).SetConcurrency(1).SaveX(context.Background())
	for i, uid := range []int64{own.ID, foreign.ID} {
		key := client.APIKey.Create().SetUserID(uid).SetKey(fmt.Sprintf("scope-%d-%d", suffix, i)).SetName("scope").SaveX(context.Background())
		client.UsageLog.Create().SetUserID(uid).SetAPIKeyID(key.ID).SetAccountID(account.ID).SetRequestID(fmt.Sprintf("scope-%d-%d", suffix, i)).SetModel("test").SetActualCost(float64(42 + i)).SetInputTokens(1).SaveX(context.Background())
	}
	statsRepo := &usageLogRepository{client: client, sql: integrationDB}
	stats, err := statsRepo.GetBatchUserUsageStats(adminCtx, []int64{own.ID, foreign.ID}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, float64(42), stats[own.ID].TotalActualCost)
	require.Zero(t, stats[foreign.ID].TotalActualCost)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM usage_logs WHERE account_id=$1", account.ID)
		_, _ = integrationDB.Exec("DELETE FROM accounts WHERE id=$1", account.ID)
	})
	// Reject overspending without either a partial credit or a ledger entry.
	_, err = svc.UpdateUserBalance(adminCtx, own.ID, 100000, "add", "unfunded")
	require.Error(t, err)
	require.Equal(t, float64(5000), client.User.GetX(context.Background(), admin.UserID).Balance)
	require.Zero(t, client.User.GetX(context.Background(), own.ID).Balance)
	// Concurrent requests share the same locked funding wallet.
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.UpdateUserBalance(adminCtx, own.ID, 1000, "add", "concurrent funding")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 5, successes)
	require.Zero(t, client.User.GetX(context.Background(), admin.UserID).Balance)
	require.Equal(t, float64(5000), client.User.GetX(context.Background(), own.ID).Balance)
	var sum float64
	var count int
	require.NoError(t, integrationDB.QueryRow("SELECT COALESCE(SUM(value),0),COUNT(*) FROM redeem_codes WHERE used_by IN($1,$2)", admin.UserID, own.ID).Scan(&sum, &count))
	require.Zero(t, sum)
	require.Equal(t, 10, count)
	_, err = svc.UpdateUserBalance(adminCtx, own.ID, 100, "subtract", "return unused funds")
	require.NoError(t, err)
	require.Equal(t, float64(100), client.User.GetX(context.Background(), admin.UserID).Balance)
	require.Equal(t, float64(4900), client.User.GetX(context.Background(), own.ID).Balance)
	// Ledger failure rolls back both wallets and the first ledger write.
	broken := &failingTransferLedger{RedeemCodeRepository: ledger}
	failingSvc := service.NewAdminService(nil, repo, nil, nil, nil, nil, broken, nil, nil, nil, nil, nil, nil, client, settings, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err = failingSvc.UpdateUserBalance(adminCtx, own.ID, 50, "add", "rollback")
	require.Error(t, err)
	require.Equal(t, float64(100), client.User.GetX(context.Background(), admin.UserID).Balance)
	require.Equal(t, float64(4900), client.User.GetX(context.Background(), own.ID).Balance)
	// Initial funding follows the same transfer and cannot manufacture credit.
	_, err = create(adminCtx, "initial-too-much", authz.User, 101)
	require.Error(t, err)
	count, err = client.User.Query().Count(context.Background())
	require.NoError(t, err)
	require.Greater(t, count, 0)
	var rejected int
	require.NoError(t, integrationDB.QueryRow("SELECT COUNT(*) FROM users WHERE email=$1", fmt.Sprintf("initial-too-much-%d@example.com", suffix)).Scan(&rejected))
	require.Zero(t, rejected)
}
