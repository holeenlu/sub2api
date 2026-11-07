package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func capacityShedCooldownFailoverErr() *service.UpstreamFailoverError {
	// service 在账号级容量降载冷却后构造出的错误形态：仍是请求级瞬时故障，但不再同账号重试。
	return &service.UpstreamFailoverError{
		StatusCode:             http.StatusServiceUnavailable,
		ResponseBody:           []byte(`{"error":{"type":"server_error","message":"Our servers are currently overloaded. Please try again later."}}`),
		RetryableOnSameAccount: false,
		RequestScopedTransient: true,
	}
}

// 账号级降载冷却后的 503 必须直接换号：账号绑定恢复路径不得把同账号重试重新打开，
// 而普通 5xx 的“至多一次同账号重试”放宽保持不变。
func TestOpenAIRecoveryPreservesCapacityShedDecision(t *testing.T) {
	for _, shed := range []bool{true, false} {
		name := "ordinary failure"
		if shed {
			name = "capacity cooldown"
		}
		t.Run(name, func(t *testing.T) {
			failure := &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway, SameAccountRetryDelay: time.Nanosecond}
			want := execution.RecoveryRetrySameAccount
			if shed {
				failure = capacityShedCooldownFailoverErr()
				require.True(t, failure.IsOpenAICapacityShed())
				want = execution.RecoveryContinue
			}
			account := &service.Account{ID: 100, Extra: map[string]any{"pool_mode": true, "pool_mode_retry_count": 3}}
			state := NewFailoverState(3, false)
			scheduler := &openAIRecoverySchedulerStub{}
			action, err := state.RecoverOpenAI(service.EnsureOpenAITurnBudget(t.Context(), nil), nil, account, failure, true, true, scheduler, nil)
			require.NoError(t, err)
			require.Equal(t, want, action)
			require.Equal(t, !shed, failure.RetryableOnSameAccount)
			if shed {
				require.Zero(t, state.SameAccountRetryCount[account.ID])
				require.Equal(t, 1, state.SwitchCount)
			} else {
				require.Equal(t, 1, state.SameAccountRetryCount[account.ID])
				require.Zero(t, state.SwitchCount)
			}
		})
	}
}

func TestHandleFailoverError_CapacityShedCooldownSwitchesWithoutSameAccountRetry(t *testing.T) {
	mock := &mockTempUnscheduler{}
	fs := NewFailoverState(3, false)

	action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, capacityShedCooldownFailoverErr())

	require.Equal(t, execution.RecoveryContinue, action)
	require.Zero(t, fs.SameAccountRetryCount[100], "no same-account backoff once the account is cooling")
	require.Equal(t, 1, fs.SwitchCount)
	require.Contains(t, fs.FailedAccountIDs, int64(100))
	require.Empty(t, mock.calls, "request-scoped failures never temp-unschedule the account")
}
