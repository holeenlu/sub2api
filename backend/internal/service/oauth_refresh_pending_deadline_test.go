package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type pendingRefreshDeadline struct {
	context.Context
	deadline time.Time
}

func (c *pendingRefreshDeadline) Deadline() (time.Time, bool) { return c.deadline, true }
func (*pendingRefreshDeadline) Done() <-chan struct{}         { return nil }
func (*pendingRefreshDeadline) Err() error                    { return nil }

type lateRefreshRepository struct {
	AccountRepository
	account *Account
	writes  int
}

func (r *lateRefreshRepository) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *lateRefreshRepository) UpdateCredentials(context.Context, int64, map[string]any) error {
	r.writes++
	return nil
}

type lateRefreshExecutor struct {
	ctx   *pendingRefreshDeadline
	calls int
}

func (*lateRefreshExecutor) CacheKey(*Account) string                  { return "synthetic-pending-deadline" }
func (*lateRefreshExecutor) CanRefresh(*Account) bool                  { return true }
func (*lateRefreshExecutor) NeedsRefresh(*Account, time.Duration) bool { return true }
func (e *lateRefreshExecutor) Refresh(context.Context, *Account) (map[string]any, error) {
	e.calls++
	e.ctx.deadline = time.Now().Add(-time.Second)
	return map[string]any{"access_token": "synthetic-only"}, nil
}
func TestOAuthRefreshRejectsPendingDeadlineBeforePersistence(t *testing.T) {
	ctx := &pendingRefreshDeadline{Context: context.Background(), deadline: time.Now().Add(time.Hour)}
	account := &Account{ID: 91, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive}
	repo := &lateRefreshRepository{account: account}
	executor := &lateRefreshExecutor{ctx: ctx}
	result, err := NewOAuthRefreshAPI(repo, nil).RefreshIfNeeded(ctx, account, executor, time.Hour)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, result)
	require.Equal(t, 1, executor.calls)
	require.Zero(t, repo.writes)
}
func TestRefreshContextErrRecognizesPendingDeadlineAndCancellation(t *testing.T) {
	ctx := &pendingRefreshDeadline{Context: context.Background(), deadline: time.Now().Add(-time.Second)}
	require.ErrorIs(t, refreshContextErr(ctx), context.DeadlineExceeded)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, refreshContextErr(canceled), context.Canceled)
	require.NoError(t, refreshContextErr(context.Background()))
}
