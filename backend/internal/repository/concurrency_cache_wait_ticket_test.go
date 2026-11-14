//go:build unit

package repository

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
)

func TestCapacityWaitTicketAtomicFIFO(t *testing.T) {
	server := miniredis.RunT(t)
	server.SetTime(time.Now()) // all contenders may have identical Redis TIME
	checkCapacityWaitTicketFIFO(t, waitObservationTestClients(t, server.Addr()), 1701)
	checkCapacityWaitTicketLateGrant(t, waitObservationTestClients(t, server.Addr()), 1702)
	checkCapacityWaitTicketMetadataBound(t, waitObservationTestClients(t, server.Addr()), 1703)
}

func TestRealRedisCapacityWaitTicketAtomicFIFO(t *testing.T) {
	if *capacityRedisTestAddr == "" {
		t.Skip("requires explicitly supplied isolated -capacity-redis-addr")
	}
	host, _, err := net.SplitHostPort(*capacityRedisTestAddr)
	require.NoError(t, err)
	require.True(t, net.ParseIP(host).IsLoopback())
	caches := waitObservationTestClients(t, *capacityRedisTestAddr)
	require.NoError(t, caches[0].rdb.Ping(t.Context()).Err())
	checkCapacityWaitTicketFIFO(t, caches, time.Now().UnixNano())
	checkCapacityWaitTicketLateGrant(t, caches, time.Now().UnixNano())
	checkCapacityWaitTicketMetadataBound(t, caches, time.Now().UnixNano())
}

func checkCapacityWaitTicketMetadataBound(t *testing.T, caches [2]*concurrencyCache, id int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	request := routing.CapacityRequest{ID: id, Token: "new-at-hard-cap", Wait: true, Limit: 1}
	keys := capacityWaitKeys(request)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = caches[0].rdb.Del(cleanup, keys...).Err()
		_ = caches[0].rdb.ZRem(cleanup, accountActiveIndexKey, strconv.FormatInt(id, 10)).Err()
	})
	now, err := caches[0].rdb.Time(ctx).Result()
	require.NoError(t, err)
	states := make(map[string]any, 4096)
	deadlines := make([]redis.Z, 4096)
	for i := range deadlines {
		token := fmt.Sprintf("terminal-%04d", i)
		states[token] = "closed"
		deadlines[i] = redis.Z{Score: float64(now.Add(time.Minute).UnixMilli()), Member: token}
	}
	require.NoError(t, caches[0].rdb.HSet(ctx, keys[4], states).Err())
	require.NoError(t, caches[0].rdb.ZAdd(ctx, keys[3], deadlines...).Err())
	ticket, err := routing.EnqueueCapacityWait(ctx, caches[0], request, 20, nil)
	require.ErrorIs(t, err, routing.ErrCapacityWaitFull)
	require.Nil(t, ticket)
	require.Equal(t, int64(4096), caches[0].rdb.HLen(ctx, keys[4]).Val(), "definite rejection must not create a tombstone")
	require.False(t, caches[0].rdb.HExists(ctx, keys[4], request.Token).Val())
	require.ErrorIs(t, caches[0].EndCapacityWait(ctx, request, true), routing.ErrCapacityDependencyUnavailable)
	require.Equal(t, int64(4096), caches[0].rdb.HLen(ctx, keys[4]).Val(), "uncertain cleanup also has a hard memory bound")
	closed := request
	closed.Token = "terminal-0000"
	deadline := caches[0].rdb.ZScore(ctx, keys[3], closed.Token).Val()
	ticket, err = routing.EnqueueCapacityWait(ctx, caches[0], closed, 20, nil)
	require.ErrorIs(t, err, routing.ErrCapacityWaitExpired)
	require.Nil(t, ticket)
	require.Equal(t, deadline, caches[0].rdb.ZScore(ctx, keys[3], closed.Token).Val())
	for i := range deadlines {
		deadlines[i].Score = float64(now.Add(-time.Second).UnixMilli())
	}
	require.NoError(t, caches[0].rdb.ZAdd(ctx, keys[3], deadlines...).Err())
	ok, err := caches[0].AcquireAccountSlot(ctx, id, 1, "fast-after-expiry")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(4096-128), caches[0].rdb.HLen(ctx, keys[4]).Val(), "each script reaps only its bounded batch")
}

type lateGrantWaitStore struct {
	*concurrencyCache
	afterGrant func(context.Context)
}

func (s *lateGrantWaitStore) AcquireQueuedCapacity(ctx context.Context, request routing.CapacityRequest) (bool, error) {
	ok, err := s.concurrencyCache.AcquireQueuedCapacity(ctx, request)
	if ok && err == nil {
		s.afterGrant(ctx)
	}
	return ok, err
}

func checkCapacityWaitTicketLateGrant(t *testing.T, caches [2]*concurrencyCache, id int64) {
	t.Helper()
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprintf("late-grant-timeout=%t", timeout), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			request := routing.CapacityRequest{ID: id, Token: fmt.Sprintf("late-%t", timeout), Limit: 1, Wait: true}
			keys := capacityWaitKeys(request)
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				_ = caches[0].rdb.Del(cleanup, keys...).Err()
				_ = caches[0].rdb.ZRem(cleanup, accountActiveIndexKey, strconv.FormatInt(id, 10)).Err()
			})
			granted := false
			store := &lateGrantWaitStore{concurrencyCache: caches[0], afterGrant: func(ctx context.Context) {
				granted = true
				if timeout {
					<-ctx.Done()
				} else {
					cancel()
				}
			}}
			ticket, err := routing.EnqueueCapacityWait(ctx, store, request, 2, nil)
			require.NoError(t, err)
			defer ticket.Close()
			result, err := ticket.Acquire(ctx, 1, nil)
			require.True(t, granted, "the actual Lua must commit before cancellation")
			require.Error(t, err)
			require.Nil(t, result)
			ticket.Close()
			cleanup, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			require.Zero(t, caches[1].rdb.ZCard(cleanup, keys[0]).Val(), "late capacity must be rolled back")
			require.Zero(t, caches[1].rdb.ZCard(cleanup, keys[2]).Val(), "only this wait position is retired")
			require.ErrorIs(t, caches[1].EnqueueCapacityWait(cleanup, request, 2, time.Minute), routing.ErrCapacityWaitExpired)
		})
	}
}

func checkCapacityWaitTicketFIFO(t *testing.T, caches [2]*concurrencyCache, id int64) {
	t.Helper()
	for _, user := range []bool{false, true} {
		t.Run(fmt.Sprintf("user=%t", user), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			first := routing.CapacityRequest{ID: id, Token: "z-first", User: user, Limit: 1, Wait: true}
			second := first
			second.Token = "a-second"
			keys := capacityWaitKeys(first)
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				_ = caches[0].rdb.Del(cleanup, keys...).Err()
				_ = caches[0].rdb.ZRem(cleanup, accountActiveIndexKey, strconv.FormatInt(id, 10)).Err()
				_ = caches[0].rdb.ZRem(cleanup, userActiveIndexKey, strconv.FormatInt(id, 10)).Err()
			})
			acquire, release := caches[0].AcquireAccountSlot, caches[0].ReleaseAccountSlot
			if user {
				acquire, release = caches[0].AcquireUserSlot, caches[0].ReleaseUserSlot
			}
			ok, err := acquire(ctx, id, 1, "holder")
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, caches[0].EnqueueCapacityWait(ctx, first, 2, time.Minute))
			deadline := caches[0].rdb.ZScore(ctx, keys[3], first.Token).Val()
			require.NoError(t, caches[1].EnqueueCapacityWait(ctx, second, 2, time.Minute))
			require.NoError(t, caches[1].EnqueueCapacityWait(ctx, first, 2, time.Hour))
			require.Equal(t, deadline, caches[1].rdb.ZScore(ctx, keys[3], first.Token).Val())
			overflow := first
			overflow.Token = "overflow"
			require.ErrorIs(t, caches[0].EnqueueCapacityWait(ctx, overflow, 2, time.Minute), routing.ErrCapacityWaitFull)
			require.NoError(t, release(ctx, id, "holder"))

			// Free capacity exists. Tail and fast-path clients race the head;
			// only the head may win, regardless of client scheduling or token sort.
			type outcome struct {
				token string
				ok    bool
				err   error
			}
			start := make(chan struct{})
			results := make(chan outcome, 34)
			var wg sync.WaitGroup
			for i := range 34 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					request := first
					cache := caches[i%2]
					if i == 1 {
						request = second
					}
					if i > 1 {
						request.Token = fmt.Sprintf("new-%d", i)
						request.Wait = false
					}
					var won bool
					var err error
					if request.Wait {
						won, err = cache.AcquireQueuedCapacity(ctx, request)
					} else if user {
						won, err = cache.AcquireUserSlot(ctx, id, 1, request.Token)
					} else {
						won, err = cache.AcquireAccountSlot(ctx, id, 1, request.Token)
					}
					results <- outcome{request.Token, won, err}
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			wins := []string{}
			for result := range results {
				require.NoError(t, result.err)
				if result.ok {
					wins = append(wins, result.token)
				}
			}
			require.Equal(t, []string{first.Token}, wins)
			require.Equal(t, []string{second.Token}, caches[0].rdb.ZRange(ctx, keys[2], 0, -1).Val())
			require.NoError(t, caches[0].EndCapacityWait(ctx, first, false))
			require.Equal(t, int64(1), caches[0].rdb.ZCard(ctx, keys[0]).Val(), "queue Close must not release transferred capacity")
			require.NoError(t, release(ctx, id, first.Token))
			ok, err = acquire(ctx, id, 1, "late-fast-path")
			require.NoError(t, err)
			require.False(t, ok)
			ok, err = caches[1].AcquireQueuedCapacity(ctx, second)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, release(ctx, id, second.Token))
			require.NoError(t, caches[1].EndCapacityWait(ctx, second, false))
			require.ErrorIs(t, caches[0].EnqueueCapacityWait(ctx, first, 2, time.Minute), routing.ErrCapacityWaitExpired)

			// Simulate a committed queued grant with a lost response, then cancel.
			ok, err = acquire(ctx, id, 2, "unrelated-owner")
			require.NoError(t, err)
			require.True(t, ok)
			lost := first
			lost.Token = "lost-reply"
			lost.Limit = 2
			neighbor := lost
			neighbor.Token = "neighbor"
			require.NoError(t, caches[0].EnqueueCapacityWait(ctx, lost, 2, time.Minute))
			require.NoError(t, caches[1].EnqueueCapacityWait(ctx, neighbor, 2, time.Minute))
			ok, err = caches[0].AcquireQueuedCapacity(ctx, lost)
			require.NoError(t, err)
			require.True(t, ok)
			var closes sync.WaitGroup
			errs := make(chan error, 16)
			for i := range 16 {
				closes.Add(1)
				go func() { defer closes.Done(); errs <- caches[i%2].EndCapacityWait(ctx, lost, true) }()
			}
			closes.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			require.Equal(t, []string{"unrelated-owner"}, caches[0].rdb.ZRange(ctx, keys[0], 0, -1).Val())
			require.Equal(t, []string{neighbor.Token}, caches[0].rdb.ZRange(ctx, keys[2], 0, -1).Val())
			require.ErrorIs(t, caches[1].EnqueueCapacityWait(ctx, lost, 2, time.Minute), routing.ErrCapacityWaitExpired)
			unknown := first
			unknown.Token = "cancel-before-delayed-enqueue"
			require.NoError(t, caches[1].EndCapacityWait(ctx, unknown, true))
			require.ErrorIs(t, caches[0].EnqueueCapacityWait(ctx, unknown, 2, time.Minute), routing.ErrCapacityWaitExpired)
		})
	}
}

func TestCapacityWaitTicketExpiryAndIndependentResources(t *testing.T) {
	server := miniredis.RunT(t)
	caches := waitObservationTestClients(t, server.Addr())
	ctx := t.Context()
	now := time.Now().Truncate(time.Millisecond)
	server.SetTime(now)
	request := routing.CapacityRequest{ID: 19, Token: "expires", Limit: 1, Wait: true}
	require.NoError(t, caches[0].EnqueueCapacityWait(ctx, request, 1, 2*time.Second))
	server.SetTime(now.Add(time.Second))
	require.NoError(t, caches[1].EnqueueCapacityWait(ctx, request, 1, time.Hour))
	for _, other := range []routing.CapacityRequest{{ID: 20, Token: "other-account", Limit: 1}, {ID: 19, User: true, Token: "user", Limit: 1}} {
		ok, err := caches[1].acquireCapacity(ctx, other)
		require.NoError(t, err)
		require.True(t, ok)
	}
	require.NoError(t, caches[0].TrackAPIKeySlot(ctx, 19, "stats-only"))
	require.Equal(t, int64(1), caches[0].rdb.ZCard(ctx, apiKeySlotKey(19)).Val())
	server.SetTime(now.Add(2 * time.Second))
	ok, err := caches[1].AcquireAccountSlot(ctx, 19, 1, "after-expiry")
	require.NoError(t, err)
	require.True(t, ok, "an expired head cannot block the resource")
	ok, err = caches[0].AcquireQueuedCapacity(ctx, request)
	require.ErrorIs(t, err, routing.ErrCapacityWaitExpired)
	require.False(t, ok)
}

func TestCapacityWaitTicketPreservesLiveAndAPIKeySemantics(t *testing.T) {
	server := miniredis.RunT(t)
	cache := waitObservationTestClients(t, server.Addr())[0]
	ctx := t.Context()
	ok, err := cache.AcquireLiveLease(ctx, 21, 1, 22, 1, 23, "live", false)
	require.NoError(t, err)
	require.True(t, ok)
	requests := []routing.CapacityRequest{{ID: 21, Token: "account-wait", Limit: 1, Wait: true}, {ID: 22, Token: "user-wait", Limit: 1, Wait: true, User: true}}
	for _, request := range requests {
		require.NoError(t, cache.EnqueueCapacityWait(ctx, request, 20, time.Minute))
		ok, err := cache.AcquireQueuedCapacity(ctx, request)
		require.NoError(t, err)
		require.False(t, ok, "live capacity still counts against regular slots")
	}
	require.NoError(t, cache.TrackAPIKeySlot(ctx, 23, "stats-only"))
	counts, err := cache.GetAPIKeyConcurrencyBatch(ctx, []int64{23})
	require.NoError(t, err)
	require.Equal(t, 2, counts[23])
	ok, err = cache.RefreshLiveLease(ctx, 21, 22, 23, "live")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, cache.ReleaseLiveLease(ctx, 21, 22, 23, "live"))
	for _, request := range requests {
		ok, err := cache.AcquireQueuedCapacity(ctx, request)
		require.NoError(t, err)
		require.True(t, ok)
	}
	counts, err = cache.GetAPIKeyConcurrencyBatch(ctx, []int64{23})
	require.NoError(t, err)
	require.Equal(t, 1, counts[23], "waiting is never represented as a key slot")
}
