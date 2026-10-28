//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type balanceUserRepoStub struct {
	*userRepoStub
	adjustErr error
	// changes 记录每次原子余额变更，顺序与调用顺序一致。
	changes []BalanceChange
}

func (s *balanceUserRepoStub) AdjustBalance(ctx context.Context, id int64, delta float64) (BalanceChange, error) {
	return s.apply(func(current float64) float64 { return current + delta })
}

func (s *balanceUserRepoStub) SetBalance(ctx context.Context, id int64, value float64) (BalanceChange, error) {
	return s.apply(func(float64) float64 { return value })
}

func (s *balanceUserRepoStub) apply(next func(current float64) float64) (BalanceChange, error) {
	if s.adjustErr != nil {
		return BalanceChange{}, s.adjustErr
	}
	if s.userRepoStub == nil || s.userRepoStub.user == nil {
		return BalanceChange{}, ErrUserNotFound
	}
	change := BalanceChange{Old: s.userRepoStub.user.Balance}
	change.New = next(change.Old)
	if change.New < 0 {
		return change, ErrBalanceNegative
	}
	s.userRepoStub.user.Balance = change.New
	s.changes = append(s.changes, change)
	return change, nil
}

type balanceRedeemRepoStub struct {
	*redeemRepoStub
	created []*RedeemCode
}

func (s *balanceRedeemRepoStub) Create(ctx context.Context, code *RedeemCode) error {
	if code == nil {
		return nil
	}
	clone := *code
	s.created = append(s.created, &clone)
	return nil
}

type authCacheInvalidatorStub struct {
	userIDs  []int64
	groupIDs []int64
	keys     []string
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByKey(ctx context.Context, key string) {
	s.keys = append(s.keys, key)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {
	s.groupIDs = append(s.groupIDs, groupID)
}

// 管理员调账必须走原子的 AdjustBalance/SetBalance，而不是"读余额→算新值→整行写回"，
// 后者会把并发的计费扣款覆盖掉。userRepoStub.Update 对未预期的调用会 panic，
// 因此这里同时证明它没被走到。
func TestAdminService_UpdateUserBalance_UsesAtomicPrimitives(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		amount    float64
		want      BalanceChange
	}{
		{name: "add", operation: "add", amount: 5, want: BalanceChange{Old: 10, New: 15}},
		{name: "subtract", operation: "subtract", amount: 4, want: BalanceChange{Old: 10, New: 6}},
		{name: "set", operation: "set", amount: 2, want: BalanceChange{Old: 10, New: 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &balanceUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 7, Balance: 10}}}
			svc := &adminServiceImpl{
				userRepo:       repo,
				redeemCodeRepo: &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}},
			}

			user, err := svc.UpdateUserBalance(context.Background(), 7, tt.amount, tt.operation, "")
			require.NoError(t, err)
			require.Equal(t, []BalanceChange{tt.want}, repo.changes)
			require.Equal(t, tt.want.New, user.Balance)
		})
	}
}

func TestAdminService_UpdateUserBalance_RejectsNegativeResult(t *testing.T) {
	repo := &balanceUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 7, Balance: 3}}}
	svc := &adminServiceImpl{
		userRepo:       repo,
		redeemCodeRepo: &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}},
	}

	_, err := svc.UpdateUserBalance(context.Background(), 7, 4, "subtract", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "balance cannot be negative")
	require.Empty(t, repo.changes, "refused adjustment must not be applied")
	require.Equal(t, 3.0, repo.userRepoStub.user.Balance)
}

func TestAdminService_UpdateUserBalance_RejectsUnknownOperation(t *testing.T) {
	repo := &balanceUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 7, Balance: 10}}}
	svc := &adminServiceImpl{
		userRepo:       repo,
		redeemCodeRepo: &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}},
	}

	_, err := svc.UpdateUserBalance(context.Background(), 7, 1, "multiply", "")
	require.Error(t, err)
	require.Empty(t, repo.changes)
}

func TestAdminService_UpdateUserBalance_InvalidatesAuthCache(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       redeemRepo,
		authCacheInvalidator: invalidator,
	}

	_, err := svc.UpdateUserBalance(context.Background(), 7, 5, "add", "")
	require.NoError(t, err)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Len(t, redeemRepo.created, 1)
}

func TestAdminService_UpdateUserBalance_NoChangeNoInvalidate(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       redeemRepo,
		authCacheInvalidator: invalidator,
	}

	_, err := svc.UpdateUserBalance(context.Background(), 7, 10, "set", "")
	require.NoError(t, err)
	require.Empty(t, invalidator.userIDs)
	require.Empty(t, redeemRepo.created)
}

type balanceInvalidationCacheStub struct {
	BillingCache
	invalidate func(context.Context, int64) error
}

func (s *balanceInvalidationCacheStub) InvalidateUserBalance(ctx context.Context, userID int64) error {
	return s.invalidate(ctx, userID)
}

func TestAdminService_UpdateUserBalance_JoinsBillingInvalidationAfterRequestCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type requestValueKey struct{}
		ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), requestValueKey{}, "balance-adjustment"), time.Second)
		defer cancel()
		repo := &balanceUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 7, Balance: 10}}}
		redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
		entered := make(chan context.Context, 1)
		release := make(chan struct{})
		releaseCache := sync.OnceFunc(func() { close(release) })
		defer func() {
			releaseCache()
			synctest.Wait()
		}()
		var cacheUserID int64
		cache := &balanceInvalidationCacheStub{invalidate: func(cacheCtx context.Context, userID int64) error {
			cacheUserID = userID
			entered <- cacheCtx
			<-release
			return nil
		}}
		svc := &adminServiceImpl{
			userRepo: repo, redeemCodeRepo: redeemRepo,
			billingCacheService: &BillingCacheService{cache: cache},
		}
		type outcome struct {
			user *User
			err  error
		}
		result := make(chan outcome, 1)
		go func() {
			user, err := svc.UpdateUserBalance(ctx, 7, 5, "add", "owned invalidation")
			result <- outcome{user, err}
		}()
		synctest.Wait()
		var cacheCtx context.Context
		select {
		case cacheCtx = <-entered:
		default:
			t.Fatal("balance mutation never reached billing cache invalidation")
		}
		// Wait proves the service goroutine is blocked; an empty result channel
		// here cannot be explained by the scheduler merely not running it yet.
		select {
		case <-result:
			t.Fatal("balance update returned while billing invalidation was still in flight")
		default:
		}
		require.Equal(t, int64(7), cacheUserID)
		require.Equal(t, []BalanceChange{{Old: 10, New: 15}}, repo.changes)
		require.Len(t, redeemRepo.created, 1, "adjustment bookkeeping must precede cache cleanup")
		require.Equal(t, "balance-adjustment", cacheCtx.Value(requestValueKey{}))
		deadline, ok := cacheCtx.Deadline()
		require.True(t, ok)
		require.Equal(t, 5*time.Second, deadline.Sub(time.Now()), "cleanup keeps its existing budget, independently of the request deadline")

		cancel()
		synctest.Wait()
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.NoError(t, cacheCtx.Err(), "cancellation after commit must not abandon invalidation")
		select {
		case <-result:
			t.Fatal("request cancellation released the service before invalidation joined")
		default:
		}

		releaseCache()
		synctest.Wait()
		got := <-result
		require.NoError(t, got.err)
		require.Equal(t, float64(15), got.user.Balance)
		require.ErrorIs(t, cacheCtx.Err(), context.Canceled, "completed cleanup releases its timeout context")
		require.Len(t, repo.changes, 1)
		require.Len(t, redeemRepo.created, 1)
	})
}

func TestAdminService_UpdateUserBalance_BillingInvalidationDeadlinePreservesCommit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &balanceUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 7, Balance: 10}}}
		redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
		var invalidationErr error
		calls := 0
		cache := &balanceInvalidationCacheStub{invalidate: func(ctx context.Context, _ int64) error {
			calls++
			<-ctx.Done()
			invalidationErr = ctx.Err()
			return invalidationErr
		}}
		svc := &adminServiceImpl{
			userRepo: repo, redeemCodeRepo: redeemRepo,
			billingCacheService: &BillingCacheService{cache: cache},
		}
		started := time.Now()
		// Waiting for the real cleanup context advances only synctest's virtual
		// clock. No sleep or wall-clock scheduling assumption establishes join.
		user, err := svc.UpdateUserBalance(t.Context(), 7, 5, "add", "bounded invalidation")
		require.NoError(t, err, "a cache timeout must not turn the committed mutation into a retryable failure")
		require.Equal(t, float64(15), user.Balance)
		require.ErrorIs(t, invalidationErr, context.DeadlineExceeded)
		require.Equal(t, 5*time.Second, time.Since(started))
		require.Equal(t, 1, calls)
		require.Equal(t, []BalanceChange{{Old: 10, New: 15}}, repo.changes)
		require.Len(t, redeemRepo.created, 1)
	})
}

func TestAdminService_UpdateUserBalance_BillingInvalidationFailurePreservesCommit(t *testing.T) {
	repo := &balanceUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 7, Balance: 10}}}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	failure := errors.New("billing cache unavailable")
	var cacheCtx context.Context
	calls := 0
	cache := &balanceInvalidationCacheStub{invalidate: func(ctx context.Context, _ int64) error {
		cacheCtx = ctx
		calls++
		return failure
	}}
	svc := &adminServiceImpl{
		userRepo: repo, redeemCodeRepo: redeemRepo,
		billingCacheService: &BillingCacheService{cache: cache},
	}
	user, err := svc.UpdateUserBalance(t.Context(), 7, 5, "add", "failed invalidation")
	require.NoError(t, err, "the existing cache failure policy preserves the committed result")
	require.Equal(t, float64(15), user.Balance)
	require.Equal(t, 1, calls)
	require.NotNil(t, cacheCtx)
	require.ErrorIs(t, cacheCtx.Err(), context.Canceled)
	require.Equal(t, []BalanceChange{{Old: 10, New: 15}}, repo.changes)
	require.Len(t, redeemRepo.created, 1)
}

type balanceReadAfterWriteFailureRepo struct {
	*balanceUserRepoStub
	readErr error
	reads   int
}

func (s *balanceReadAfterWriteFailureRepo) GetByID(ctx context.Context, userID int64) (*User, error) {
	s.reads++
	if len(s.changes) > 0 {
		return nil, s.readErr
	}
	return s.userRepoStub.GetByID(ctx, userID)
}

func TestAdminService_UpdateUserBalance_ReadAfterWriteFailureStillJoinsInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cacheErr error
	}{
		{name: "cache_success"},
		{name: "cache_failure", cacheErr: errors.New("billing cache unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				readErr := errors.New("read after committed balance update failed")
				repo := &balanceReadAfterWriteFailureRepo{
					balanceUserRepoStub: &balanceUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 7, Balance: 10}}},
					readErr:             readErr,
				}
				redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
				release := make(chan struct{})
				releaseCache := sync.OnceFunc(func() { close(release) })
				defer func() {
					releaseCache()
					synctest.Wait()
				}()
				var invalidatedUsers []int64
				cache := &balanceInvalidationCacheStub{invalidate: func(_ context.Context, userID int64) error {
					invalidatedUsers = append(invalidatedUsers, userID)
					<-release
					return tc.cacheErr
				}}
				svc := &adminServiceImpl{
					userRepo: repo, redeemCodeRepo: redeemRepo,
					billingCacheService: &BillingCacheService{cache: cache},
				}
				type outcome struct {
					user *User
					err  error
				}
				result := make(chan outcome, 1)
				go func() {
					user, err := svc.UpdateUserBalance(t.Context(), 7, 5, "add", "failed post-commit read")
					result <- outcome{user, err}
				}()
				synctest.Wait()
				require.Equal(t, 2, repo.reads, "the admission read succeeds and the post-commit read fails")
				require.Equal(t, []BalanceChange{{Old: 10, New: 15}}, repo.changes)
				require.Equal(t, []int64{7}, invalidatedUsers, "a failed read must not skip post-commit invalidation")
				select {
				case <-result:
					t.Fatal("read failure returned while post-commit invalidation was still in flight")
				default:
				}
				require.Empty(t, redeemRepo.created, "failed user read must not fabricate adjustment bookkeeping")

				releaseCache()
				synctest.Wait()
				got := <-result
				require.Nil(t, got.user)
				require.Same(t, readErr, got.err, "cache cleanup must preserve the original read error")
				require.Equal(t, []int64{7}, invalidatedUsers)
				require.Equal(t, []BalanceChange{{Old: 10, New: 15}}, repo.changes)
				require.Equal(t, float64(15), repo.userRepoStub.user.Balance)
				require.Empty(t, redeemRepo.created)
			})
		})
	}
}
