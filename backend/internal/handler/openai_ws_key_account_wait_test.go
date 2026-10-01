package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Keep the local bound-account wait when adding key queues. No user slot or
// pricing snapshot may be held during that wait, and a disconnect must reclaim
// the newly acquired key reservation before another request can enter.
func TestOpenAIWSKeyAdmissionPreservesBoundAccountWait(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		name := "grant"
		if disconnect {
			name = "disconnect"
		}
		t.Run(name, func(t *testing.T) {
			helper, cache := newAPIKeyAdmissionHelper(t)
			helper.concurrencyService.SetAPIKeyQueuePolicy(service.APIKeyQueuePolicy{MaxWaiting: 5, Timeout: 5 * time.Second})
			queue := cache.(service.APIKeySlotQueueCache)
			holder, err := helper.concurrencyService.AcquireAccountSlot(context.Background(), 42, 1)
			require.NoError(t, err)
			require.True(t, holder.Acquired)
			defer holder.ReleaseFunc()
			client, server := startHandlerKeyQueueConn(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			service.EnsureOpenAIWSIngressReader(c, server)
			ctx, cancel := service.WithAPIKeyAdmissionOwner(context.Background())
			defer cancel()
			h := &OpenAIGatewayHandler{concurrencyHelper: helper}
			var pricing openAIWSTurnPricing
			type result struct {
				user, account func()
				err           error
			}
			done := make(chan result, 1)
			go func() {
				u, a, err := h.admitOpenAIWSTurnForPricing(ctx, c, 43, 1,
					&service.APIKey{ID: 44, ConcurrencyLimit: 1}, &service.Account{ID: 42},
					1, 5, 5*time.Second, 2, zap.NewNop(), &pricing)
				done <- result{u, a, err}
			}()
			require.Eventually(t, func() bool {
				waiting, err := cache.GetAccountWaitingCount(context.Background(), 42)
				return err == nil && waiting == 1
			}, 3*time.Second, 10*time.Millisecond)
			users, err := cache.GetUserConcurrency(context.Background(), 43)
			require.NoError(t, err)
			require.Zero(t, users, "account waiting must not hold the user slot")
			require.True(t, pricing.currentOr(time.Time{}).IsZero())
			grantAt := time.Now()
			if disconnect {
				require.NoError(t, client.CloseNow())
			} else {
				holder.ReleaseFunc()
			}
			select {
			case r := <-done:
				if disconnect {
					require.True(t, service.IsOpenAIWSClientGoneError(r.err), "%v", r.err)
					require.Nil(t, r.user)
					require.Nil(t, r.account)
				} else {
					require.NoError(t, r.err)
					require.False(t, pricing.currentOr(time.Time{}).Before(grantAt))
					r.account()
					r.user()
					r.account()
					r.user()
				}
			case <-time.After(2 * time.Second):
				t.Fatal("bound-account admission did not settle promptly")
			}
			active, waiting, err := queue.GetAPIKeyQueueStats(context.Background(), 44)
			require.NoError(t, err)
			require.Zero(t, active)
			require.Zero(t, waiting)
			waiting, err = cache.GetAccountWaitingCount(context.Background(), 42)
			require.NoError(t, err)
			require.Zero(t, waiting)
		})
	}
}
