//go:build unit

package repository

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func waitObservationTestClients(t *testing.T, address string) [2]*concurrencyCache {
	t.Helper()
	var caches [2]*concurrencyCache
	for i := range caches {
		client := redis.NewClient(&redis.Options{Addr: address})
		t.Cleanup(func() { _ = client.Close() })
		caches[i] = NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	}
	return caches
}

func TestWaitObservationDuplicatePreservesArrivalOrderAndDeadline(t *testing.T) {
	server := miniredis.RunT(t)
	caches := waitObservationTestClients(t, server.Addr())
	ctx := t.Context()
	start := time.Now().Truncate(time.Millisecond)
	server.SetTime(start)
	// Both arrivals have the same millisecond; lexical token order is reversed.
	require.NoError(t, caches[0].StartWaitObservation(ctx, "account", 17, "z-first", 10*time.Second))
	require.NoError(t, caches[1].StartWaitObservation(ctx, "account", 17, "a-second", time.Minute))
	server.SetTime(start.Add(2 * time.Second))
	require.NoError(t, caches[1].StartWaitObservation(ctx, "account", 17, "z-first", time.Hour))
	rows, err := caches[0].rdb.ZRangeWithScores(ctx, waitObservationKey("account", 17), 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "z-first", strings.SplitN(rows[0].Member.(string), "|", 3)[2])
	age, older, err := caches[0].ObserveWaitAdmission(ctx, "account", 17, "a-second")
	require.NoError(t, err)
	require.Equal(t, int64(2000), age)
	require.Equal(t, 1, older)
	// Admission already removed the second waiter before its deferred End.
	require.Equal(t, int64(1), caches[0].rdb.ZCard(ctx, waitObservationKey("account", 17)).Val())
	server.SetTime(start.Add(10 * time.Second))
	age, older, err = caches[1].ObserveWaitAdmission(ctx, "account", 17, "")
	require.NoError(t, err)
	require.Zero(t, age)
	require.Zero(t, older, "duplicate registration must not extend the original deadline")
	for range 2 {
		require.NoError(t, caches[0].EndWaitObservation(ctx, "account", 17, "a-second"))
	}
}

func TestWaitObservationShortensDeadlineWithoutChangingAnotherWaiter(t *testing.T) {
	server := miniredis.RunT(t)
	caches := waitObservationTestClients(t, server.Addr())
	ctx := t.Context()
	start := time.Now().Truncate(time.Millisecond)
	server.SetTime(start)
	require.NoError(t, caches[0].StartWaitObservation(ctx, "user", 17, "short", time.Minute))
	require.NoError(t, caches[1].StartWaitObservation(ctx, "user", 17, "long", 2*time.Minute))
	server.SetTime(start.Add(time.Second))
	require.NoError(t, caches[1].StartWaitObservation(ctx, "user", 17, "short", time.Second))
	server.SetTime(start.Add(2 * time.Second))
	_, older, err := caches[0].ObserveWaitAdmission(ctx, "user", 17, "")
	require.NoError(t, err)
	require.Equal(t, 1, older)
	rows, err := caches[0].rdb.ZRange(ctx, waitObservationKey("user", 17), 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, strings.HasSuffix(rows[0], "|long"))
}

func TestWaitObservationRejectsInvalidOrExpiredRegistration(t *testing.T) {
	server := miniredis.RunT(t)
	cache := waitObservationTestClients(t, server.Addr())[0]
	for _, tc := range []struct {
		name, scope, token string
		id                 int64
		timeout            time.Duration
	}{
		{name: "expired", scope: "account", token: "a", id: 17},
		{name: "negative", scope: "account", token: "a", id: 17, timeout: -time.Second},
		{name: "submillisecond", scope: "account", token: "a", id: 17, timeout: time.Nanosecond},
		{name: "scope", scope: "tool", token: "a", id: 17, timeout: time.Second},
		{name: "identity", scope: "user", token: "a", timeout: time.Second},
		{name: "empty token", scope: "user", id: 17, timeout: time.Second},
		{name: "delimiter", scope: "user", token: "a|b", id: 17, timeout: time.Second},
		{name: "oversized token", scope: "user", token: strings.Repeat("a", 257), id: 17, timeout: time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, cache.StartWaitObservation(t.Context(), tc.scope, tc.id, tc.token, tc.timeout))
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, cache.StartWaitObservation(ctx, "account", 17, "canceled", time.Minute), context.Canceled)
	require.Empty(t, server.Keys())
}

func TestWaitObservationBoundIsNotCapacityAdmission(t *testing.T) {
	server := miniredis.RunT(t)
	cache := waitObservationTestClients(t, server.Addr())[0]
	ctx := t.Context()
	start := time.Now().Truncate(time.Millisecond)
	server.SetTime(start)
	key := waitObservationKey("account", 17)
	members := make([]redis.Z, 4096)
	for i := range members {
		members[i] = redis.Z{Score: float64(i + 1), Member: fmt.Sprintf("%d|%d|t%d", start.Add(time.Minute).UnixMilli(), start.UnixMilli(), i)}
	}
	require.NoError(t, cache.rdb.ZAdd(ctx, key, members...).Err())
	require.Error(t, cache.StartWaitObservation(ctx, "account", 17, "overflow", time.Minute))
	require.NoError(t, cache.StartWaitObservation(ctx, "account", 17, "t0", time.Hour))
	require.Equal(t, int64(4096), cache.rdb.ZCard(ctx, key).Val())
	acquired, err := cache.AcquireAccountSlot(ctx, 17, 1, "actual-capacity")
	require.NoError(t, err)
	require.True(t, acquired, "observational ordering is not FIFO admission")
	require.NoError(t, cache.ReleaseAccountSlot(ctx, 17, "actual-capacity"))
	require.NoError(t, cache.EndWaitObservation(ctx, "account", 17, "t0"))
	require.NoError(t, cache.StartWaitObservation(ctx, "account", 17, "replacement", time.Minute))
	// A late duplicate cleanup cannot remove the newly registered identity.
	require.NoError(t, cache.EndWaitObservation(ctx, "account", 17, "t0"))
	require.Equal(t, int64(4096), cache.rdb.ZCard(ctx, key).Val())
}

func TestWaitObservationConcurrentRedisScriptAndCancellation(t *testing.T) {
	server := miniredis.RunT(t)
	checkWaitObservationConcurrentClients(t, waitObservationTestClients(t, server.Addr()), 17)
}

func TestWaitObservationAdmissionRetiresBeforeCallerEndsWait(t *testing.T) {
	for _, scope := range []string{"account", "user"} {
		t.Run(scope, func(t *testing.T) {
			server := miniredis.RunT(t)
			cache := waitObservationTestClients(t, server.Addr())[0]
			svc := service.NewConcurrencyService(cache)
			ctx, end := svc.BeginWaitObservation(t.Context(), scope, 17, time.Minute)
			defer end()
			key := waitObservationKey(scope, 17)
			require.Equal(t, int64(1), cache.rdb.ZCard(ctx, key).Val())
			acquire, count := svc.AcquireAccountSlot, cache.GetAccountConcurrency
			if scope == "user" {
				acquire, count = svc.AcquireUserSlot, cache.GetUserConcurrency
			}
			result, err := acquire(ctx, 17, 1)
			require.NoError(t, err)
			require.True(t, result.Acquired)
			defer result.ReleaseFunc()
			require.NotNil(t, result.Lease, "diagnostic registration does not replace capacity authority")
			require.Zero(t, cache.rdb.ZCard(ctx, key).Val(), "admitted requests no longer inflate wait observations")
			end()
			end()
			active, err := count(ctx, 17)
			require.NoError(t, err)
			require.Equal(t, 1, active, "ending diagnostics must not release capacity")
			result.ReleaseFunc()
			active, err = count(ctx, 17)
			require.NoError(t, err)
			require.Zero(t, active)
		})
	}
}

// MAIN can run the identical Lua/concurrency contract against an explicitly
// supplied disposable Redis. This never starts Docker or reads application config.
func TestRealRedisWaitObservationConcurrentScriptAndCancellation(t *testing.T) {
	if *capacityRedisTestAddr == "" {
		t.Skip("requires -capacity-redis-addr for an isolated loopback Redis")
	}
	host, _, err := net.SplitHostPort(*capacityRedisTestAddr)
	require.NoError(t, err)
	require.True(t, net.ParseIP(host).IsLoopback())
	caches := waitObservationTestClients(t, *capacityRedisTestAddr)
	require.NoError(t, caches[0].rdb.Ping(t.Context()).Err())
	checkWaitObservationConcurrentClients(t, caches, time.Now().UnixNano())
}

func checkWaitObservationConcurrentClients(t *testing.T, caches [2]*concurrencyCache, id int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	key := waitObservationKey("account", id)
	userKey := waitObservationKey("user", id)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = caches[0].rdb.Del(cleanup, key, userKey).Err()
	})
	const writers = 32
	parallel := func(run func(int) error) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan error, writers)
		var wg sync.WaitGroup
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results <- run(i)
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
	}
	parallel(func(i int) error {
		return caches[i%2].StartWaitObservation(ctx, "account", id, "same-token", time.Minute)
	})
	require.Equal(t, int64(1), caches[0].rdb.ZCard(ctx, key).Val(), "concurrent retries register once")
	parallel(func(i int) error {
		return caches[i%2].EndWaitObservation(ctx, "account", id, "same-token")
	})
	require.Zero(t, caches[0].rdb.ZCard(ctx, key).Val())
	parallel(func(i int) error {
		return caches[i%2].StartWaitObservation(ctx, "account", id, "token-"+strconv.Itoa(i), time.Minute)
	})
	rows, err := caches[0].rdb.ZRangeWithScores(ctx, key, 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, rows, writers)
	for i, row := range rows {
		require.Equal(t, float64(i+1), row.Score, "Redis serializes arrival across independent clients")
	}
	// Admit in reverse registration order: each script must see and retire the
	// exact member atomically, even when all arrivals share the same timestamp.
	for i := len(rows) - 1; i >= 0; i-- {
		token := strings.SplitN(rows[i].Member.(string), "|", 3)[2]
		_, older, err := caches[i%2].ObserveWaitAdmission(ctx, "account", id, token)
		require.NoError(t, err)
		require.Equal(t, i, older)
	}
	require.Zero(t, caches[0].rdb.ZCard(ctx, key).Val())
	// Actual service owners share Redis, with independent account/user scopes.
	services := [2]*service.ConcurrencyService{service.NewConcurrencyService(caches[0]), service.NewConcurrencyService(caches[1])}
	canceled, stop := context.WithCancel(ctx)
	defer stop()
	_, endCanceled := services[0].BeginWaitObservation(canceled, "account", id, time.Minute)
	defer endCanceled()
	_, endSurvivor := services[1].BeginWaitObservation(ctx, "account", id, time.Minute)
	defer endSurvivor()
	_, endUser := services[1].BeginWaitObservation(ctx, "user", id, time.Minute)
	defer endUser()
	require.Equal(t, int64(2), caches[0].rdb.ZCard(ctx, key).Val())
	stop()
	parallel(func(int) error { endCanceled(); return nil })
	require.Equal(t, int64(1), caches[0].rdb.ZCard(ctx, key).Val())
	require.Equal(t, int64(1), caches[0].rdb.ZCard(ctx, userKey).Val())
	endSurvivor()
	endUser()
	require.Zero(t, caches[0].rdb.ZCard(ctx, key).Val())
	require.Zero(t, caches[0].rdb.ZCard(ctx, userKey).Val())
}
