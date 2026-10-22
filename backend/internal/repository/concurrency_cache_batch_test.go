package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type concurrencyBatchHook struct {
	redis.Hook
	sizes  []int
	failAt int
	cancel context.CancelFunc
}

func (h *concurrencyBatchHook) DialHook(next redis.DialHook) redis.DialHook          { return next }
func (h *concurrencyBatchHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }
func (h *concurrencyBatchHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		h.sizes = append(h.sizes, len(cmds))
		if h.failAt > 0 && len(h.sizes) == h.failAt {
			return fmt.Errorf("batch failed")
		}
		err := next(ctx, cmds)
		if h.cancel != nil {
			h.cancel()
		}
		return err
	}
}
func TestConcurrencyLoadBatchBoundsAndPartialFailures(t *testing.T) {
	for _, n := range []int{256, 257, 1000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			defer func() { _ = client.Close() }()
			ctx := context.Background()
			cache := NewConcurrencyCache(client, 15, 900)
			require.NoError(t, client.Set(ctx, accountWaitKeyPrefix+fmt.Sprint(n), "3", 0).Err())
			hook := &concurrencyBatchHook{}
			client.AddHook(hook)
			accounts := make([]service.AccountWithConcurrency, n)
			for i := range accounts {
				accounts[i] = service.AccountWithConcurrency{ID: int64(i + 1), MaxConcurrency: 10}
			}
			loads, err := cache.GetAccountsLoadBatch(ctx, accounts)
			require.NoError(t, err)
			require.Len(t, loads, n)
			require.Equal(t, 3, loads[int64(n)].WaitingCount)
			require.Equal(t, 30, loads[int64(n)].LoadRate)
			for _, size := range hook.sizes {
				require.LessOrEqual(t, size, 1280)
			}
			if n > 256 {
				hook.sizes = nil
				hook.failAt = 2
				loads, err = cache.GetAccountsLoadBatch(ctx, accounts)
				require.Error(t, err)
				require.Nil(t, loads, "partial load must not masquerade as full snapshot")
				hook.failAt = 0
				hook.sizes = nil
				cancelCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				hook.cancel = cancel
				loads, err = cache.GetAccountsLoadBatch(cancelCtx, accounts)
				require.ErrorIs(t, err, context.Canceled)
				require.Nil(t, loads)
			}
		})
	}
}
