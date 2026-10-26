package handler

import (
	"context"
	"net/http"
	"testing"

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
func TestAllowOpenAIAccountBoundRetry_KeepsCapacityShedSwitchDecision(t *testing.T) {
	shed := capacityShedCooldownFailoverErr()
	require.True(t, shed.IsOpenAICapacityShed())
	allowOpenAIAccountBoundRetry(shed)
	require.False(t, shed.RetryableOnSameAccount)
	require.Zero(t, shed.SameAccountRetryMax)

	generic := &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}
	allowOpenAIAccountBoundRetry(generic)
	require.True(t, generic.RetryableOnSameAccount)
	require.Equal(t, 1, generic.SameAccountRetryMax)
}

func TestHandleFailoverError_CapacityShedCooldownSwitchesWithoutSameAccountRetry(t *testing.T) {
	mock := &mockTempUnscheduler{}
	fs := NewFailoverState(3, false)

	action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", maxSameAccountRetries, capacityShedCooldownFailoverErr())

	require.Equal(t, FailoverContinue, action)
	require.Zero(t, fs.SameAccountRetryCount[100], "no same-account backoff once the account is cooling")
	require.Equal(t, 1, fs.SwitchCount)
	require.Contains(t, fs.FailedAccountIDs, int64(100))
	require.Empty(t, mock.calls, "request-scoped failures never temp-unschedule the account")
}
