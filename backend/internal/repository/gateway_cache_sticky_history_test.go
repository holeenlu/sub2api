package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newStickyHistoryTestCache(t *testing.T) (*gatewayCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &gatewayCache{rdb: rdb}, mr
}

func TestStickySessionHistoryKeyNamespaceIsolation(t *testing.T) {
	cache, _ := newStickyHistoryTestCache(t)
	ctx := context.Background()

	// 受害者会话 abc：短期键 -> 2，历史键 -> 2。
	require.NoError(t, cache.SetSessionAccountID(ctx, 1, "abc", 2, time.Hour))
	written, err := cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "abc", 2, time.Hour)
	require.NoError(t, err)
	require.True(t, written)

	// 攻击者用各种前缀形态的 sessionHash 写自己的短期键。
	for _, hostile := range []string{
		"history:abc",
		"sticky_session_history:1:abc",
		"sticky_session:1:abc",
	} {
		require.NoError(t, cache.SetSessionAccountID(ctx, 1, hostile, 999, time.Hour))
	}

	historyAccountID, err := cache.GetSessionAccountHistory(ctx, 1, "abc")
	require.NoError(t, err)
	require.Equal(t, int64(2), historyAccountID, "客户端可控的 sessionHash 不得污染别的会话的历史键")

	stickyAccountID, err := cache.GetSessionAccountID(ctx, 1, "abc")
	require.NoError(t, err)
	require.Equal(t, int64(2), stickyAccountID)

	// 反向：写历史键也不得污染同名会话的短期键。
	_, err = cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "sticky_session:1:abc", 777, time.Hour)
	require.NoError(t, err)
	stickyAccountID, err = cache.GetSessionAccountID(ctx, 1, "sticky_session:1:abc")
	require.NoError(t, err)
	require.Equal(t, int64(999), stickyAccountID)
}

// 分组隔离：同一个 sessionHash 在不同分组下互不可见。
func TestStickySessionHistoryIsScopedByGroup(t *testing.T) {
	cache, _ := newStickyHistoryTestCache(t)
	ctx := context.Background()

	_, err := cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "s", 2, time.Hour)
	require.NoError(t, err)

	_, err = cache.GetSessionAccountHistory(ctx, 2, "s")
	require.ErrorIs(t, err, service.ErrStickySessionNotFound)
}

// 「不存在或相同才写」：已经指向别的账号时保持不动。原账号被临时闸门绕开时，本次
// 请求会落到备用账号上，历史必须继续记着原账号。
func TestSetSessionAccountHistoryIfAbsentOrSame(t *testing.T) {
	cache, mr := newStickyHistoryTestCache(t)
	ctx := context.Background()

	// 不存在 -> 写入。
	written, err := cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "s", 2, time.Hour)
	require.NoError(t, err)
	require.True(t, written)

	// 相同账号 -> 写入并续期。
	mr.FastForward(30 * time.Minute)
	written, err = cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "s", 2, time.Hour)
	require.NoError(t, err)
	require.True(t, written)
	require.Greater(t, mr.TTL(buildSessionHistoryKey(1, "s")), 45*time.Minute, "相同账号必须续期")

	// 不同账号 -> 不覆盖。
	written, err = cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "s", 5, time.Hour)
	require.NoError(t, err)
	require.False(t, written)

	accountID, err := cache.GetSessionAccountHistory(ctx, 1, "s")
	require.NoError(t, err)
	require.Equal(t, int64(2), accountID)

	// 显式删除后，新账号才能接管——这是唯一该改写历史的路径。
	require.NoError(t, cache.DeleteSessionAccountHistory(ctx, 1, "s"))
	written, err = cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "s", 5, time.Hour)
	require.NoError(t, err)
	require.True(t, written)

	accountID, err = cache.GetSessionAccountHistory(ctx, 1, "s")
	require.NoError(t, err)
	require.Equal(t, int64(5), accountID)
}

// 过期后回到「未绑定」，而不是报错。
func TestSessionAccountHistoryExpires(t *testing.T) {
	cache, mr := newStickyHistoryTestCache(t)
	ctx := context.Background()

	_, err := cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "s", 2, time.Minute)
	require.NoError(t, err)

	mr.FastForward(2 * time.Minute)

	_, err = cache.GetSessionAccountHistory(ctx, 1, "s")
	require.ErrorIs(t, err, service.ErrStickySessionNotFound)
}

func TestCompareAndRefreshSessionAccountID(t *testing.T) {
	cache, mr := newStickyHistoryTestCache(t)
	ctx := context.Background()
	key := buildSessionKey(1, "s")
	written, err := cache.CompareAndRefreshSessionAccountID(ctx, 1, "s", 2, time.Hour, false)
	require.NoError(t, err)
	require.False(t, written)
	require.False(t, mr.Exists(key), "refresh-only must not recreate legacy keys")
	written, err = cache.CompareAndRefreshSessionAccountID(ctx, 1, "s", 2, time.Hour, true)
	require.NoError(t, err)
	require.True(t, written)
	mr.FastForward(45 * time.Minute)
	written, err = cache.CompareAndRefreshSessionAccountID(ctx, 1, "s", 2, time.Hour, false)
	require.NoError(t, err)
	require.True(t, written)
	require.Equal(t, time.Hour, mr.TTL(key))

	// A different request takes ownership between selection and refresh.
	require.NoError(t, cache.SetSessionAccountID(ctx, 1, "s", 5, 7*time.Minute))
	for _, create := range []bool{false, true} {
		written, err = cache.CompareAndRefreshSessionAccountID(ctx, 1, "s", 2, time.Hour, create)
		require.NoError(t, err)
		require.False(t, written)
		require.Equal(t, 7*time.Minute, mr.TTL(key))
		owner, err := cache.GetSessionAccountID(ctx, 1, "s")
		require.NoError(t, err)
		require.Equal(t, int64(5), owner)
	}
	// Existing group and history namespaces remain independent.
	written, err = cache.CompareAndRefreshSessionAccountID(ctx, 2, "s", 9, time.Hour, true)
	require.NoError(t, err)
	require.True(t, written)
	written, err = cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, "s", 7, time.Hour)
	require.NoError(t, err)
	require.True(t, written)
	mr.FastForward(8 * time.Minute)
	written, err = cache.CompareAndRefreshSessionAccountID(ctx, 1, "s", 2, time.Hour, true)
	require.NoError(t, err)
	require.True(t, written, "an admitted turn can restore an expired binding")
	owner, err := cache.GetSessionAccountID(ctx, 2, "s")
	require.NoError(t, err)
	require.Equal(t, int64(9), owner)
	owner, err = cache.GetSessionAccountHistory(ctx, 1, "s")
	require.NoError(t, err)
	require.Equal(t, int64(7), owner)
}

func TestCompareAndRefreshSessionAccountID_ConcurrentOwners(t *testing.T) {
	cache, _ := newStickyHistoryTestCache(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	type outcome struct {
		owner   int64
		written bool
		err     error
	}
	results := make(chan outcome, 2)
	for _, owner := range []int64{2, 5} {
		wg.Add(1)
		go func(owner int64) {
			defer wg.Done()
			<-start
			written, err := cache.CompareAndRefreshSessionAccountID(context.Background(), 1, "s", owner, time.Hour, true)
			results <- outcome{owner: owner, written: written, err: err}
		}(owner)
	}
	close(start)
	wg.Wait()
	close(results)
	var winner int64
	writes := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.written {
			writes++
			winner = result.owner
		}
	}
	require.Equal(t, 1, writes)
	owner, err := cache.GetSessionAccountID(context.Background(), 1, "s")
	require.NoError(t, err)
	require.Equal(t, winner, owner)
}

func TestCompareAndRefreshSessionAccountID_InvalidInput(t *testing.T) {
	cache, _ := newStickyHistoryTestCache(t)
	for _, tc := range []struct {
		hash  string
		owner int64
		ttl   time.Duration
	}{
		{"", 2, time.Hour}, {" ", 2, time.Hour}, {"s", 0, time.Hour},
		{"s", 2, 0}, {"s", 2, -time.Second}, {"s", 2, time.Nanosecond},
	} {
		written, err := cache.CompareAndRefreshSessionAccountID(context.Background(), 1, tc.hash, tc.owner, tc.ttl, true)
		require.Error(t, err)
		require.False(t, written)
	}
}

// 非法入参不得静默写坏键。
func TestSetSessionAccountHistoryRejectsInvalidInput(t *testing.T) {
	cache, _ := newStickyHistoryTestCache(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name        string
		sessionHash string
		accountID   int64
		ttl         time.Duration
	}{
		{"空 sessionHash", "", 2, time.Hour},
		{"空白 sessionHash", "   ", 2, time.Hour},
		{"非法 accountID", "s", 0, time.Hour},
		{"非法 TTL", "s", 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			written, err := cache.SetSessionAccountHistoryIfAbsentOrSame(ctx, 1, tc.sessionHash, tc.accountID, tc.ttl)
			require.Error(t, err)
			require.False(t, written)
		})
	}
}
