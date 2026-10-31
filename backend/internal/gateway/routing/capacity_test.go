package routing

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type capacityStore struct {
	acquire func(context.Context, int64, int, string) (bool, error)
	release func(context.Context, int64, string) error
	user    atomic.Bool
}

func (s *capacityStore) AcquireAccountSlot(ctx context.Context, id int64, limit int, token string) (bool, error) {
	return s.acquire(ctx, id, limit, token)
}
func (s *capacityStore) AcquireUserSlot(ctx context.Context, id int64, limit int, token string) (bool, error) {
	s.user.Store(true)
	return s.acquire(ctx, id, limit, token)
}
func (s *capacityStore) ReleaseAccountSlot(ctx context.Context, id int64, token string) error {
	return s.release(ctx, id, token)
}
func (s *capacityStore) ReleaseUserSlot(ctx context.Context, id int64, token string) error {
	return s.release(ctx, id, token)
}

func TestCapacityReleaseOwnsOnlyAcquiredTokenOnce(t *testing.T) {
	for _, user := range []bool{false, true} {
		t.Run(map[bool]string{false: "account", true: "user"}[user], func(t *testing.T) {
			var released atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			store := &capacityStore{
				acquire: func(ctx context.Context, id int64, limit int, token string) (bool, error) {
					require.EqualValues(t, 7, id)
					require.Equal(t, 3, limit)
					require.Equal(t, "r-owner-token", token)
					return true, nil
				},
				release: func(ctx context.Context, id int64, token string) error {
					require.NoError(t, ctx.Err())
					_, bounded := ctx.Deadline()
					require.True(t, bounded)
					require.EqualValues(t, 7, id)
					require.Equal(t, "r-owner-token", token)
					released.Add(1)
					return nil
				},
			}
			result, err := AcquireCapacity(ctx, store, CapacityRequest{ID: 7, Token: "r-owner-token", Limit: 3, User: user}, nil)
			require.NoError(t, err)
			require.True(t, result.Acquired)
			require.Equal(t, user, store.user.Load())
			cancel()
			var wg sync.WaitGroup
			for range 12 {
				wg.Go(result.ReleaseFunc)
			}
			wg.Wait()
			require.EqualValues(t, 1, released.Load())
		})
	}
}

func TestCapacityDisabledAndUnavailableAreDistinct(t *testing.T) {
	result, err := AcquireCapacity(t.Context(), nil, CapacityRequest{ID: 7, Limit: 0}, nil)
	require.NoError(t, err)
	require.True(t, result.Acquired)
	result.ReleaseFunc()
	_, err = AcquireCapacity(t.Context(), nil, CapacityRequest{ID: 7, Token: "t", Limit: 1}, nil)
	require.Error(t, err)
	store := &capacityStore{acquire: func(context.Context, int64, int, string) (bool, error) { return false, errors.New("redis down") }}
	result, err = AcquireCapacity(t.Context(), store, CapacityRequest{ID: 7, Token: "t", Limit: 1}, nil)
	require.Error(t, err)
	require.Nil(t, result)
}

func TestCapacityCancellationAfterAcquireReleasesBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var released atomic.Int32
	store := &capacityStore{acquire: func(context.Context, int64, int, string) (bool, error) { cancel(); return true, nil }, release: func(context.Context, int64, string) error { released.Add(1); return nil }}
	result, err := AcquireCapacity(ctx, store, CapacityRequest{ID: 7, Token: "t", Limit: 1}, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
	require.EqualValues(t, 1, released.Load())
}

func (s *capacityStore) CheckCapacityLease(ctx context.Context, _ CapacityRequest) (time.Duration, error) {
	return time.Hour, ctx.Err()
}
func (s *capacityStore) RefreshCapacityLease(ctx context.Context, _ CapacityRequest) (time.Duration, error) {
	return time.Hour, ctx.Err()
}
