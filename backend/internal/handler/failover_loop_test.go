package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock
// ---------------------------------------------------------------------------

func TestOpenAISameAccountRetrySharesTurnLimits(t *testing.T) {
	account := &service.Account{ID: 12, Extra: map[string]any{"pool_mode": true, "pool_mode_retry_count": 3}}
	failure := &service.UpstreamFailoverError{RetryableOnSameAccount: true, SameAccountRetryDelay: time.Nanosecond}
	counts := map[int64]int{}
	ctx := service.WithOpenAITurnBudget(context.Background(), service.NewOpenAITurnBudget(nil))
	retried, err := waitOpenAISameAccountRetry(ctx, account, failure, counts, nil, "")
	require.NoError(t, err)
	require.True(t, retried)
	require.Same(t, account, service.TurnRetryAccount(ctx))
	retried, err = waitOpenAISameAccountRetry(ctx, account, failure, counts, nil, "")
	require.NoError(t, err)
	require.False(t, retried, "HTTP/WS 再次进入不能重新取得同账号重试预算")
	require.Equal(t, 1, counts[account.ID])
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	counts = map[int64]int{}
	retried, err = waitOpenAISameAccountRetry(canceled, account, failure, counts, nil, "")
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, retried)
	require.Empty(t, counts)
}

// mockTempUnscheduler 记录 TempUnscheduleRetryableError 的调用信息。
type mockTempUnscheduler struct {
	calls []tempUnscheduleCall
}

type tempUnscheduleCall struct {
	accountID   int64
	failoverErr *service.UpstreamFailoverError
}

func (m *mockTempUnscheduler) TempUnscheduleRetryableError(_ context.Context, accountID int64, failoverErr *service.UpstreamFailoverError) {
	m.calls = append(m.calls, tempUnscheduleCall{accountID: accountID, failoverErr: failoverErr})
}

func TestFailoverStopsContentRefusalBeforeRetryOrAccountSideEffects(t *testing.T) {
	for _, body := range []string{
		`{"stop_reason":"refusal"}`,
		`{"error":{"code":"content_policy_violation"}}`,
		`{"choices":[{"finish_reason":"content_filter"}]}`,
		`{"type":"response.refusal.done","refusal":"declined"}`,
	} {
		t.Run(body, func(t *testing.T) {
			for _, sameAccount := range []bool{true, false} {
				ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
				owner := &service.Account{ID: 41}
				service.PinTurnRetryAccount(ctx, owner)
				state := NewFailoverState(5, true)
				unscheduler := &mockTempUnscheduler{}
				failure := &service.UpstreamFailoverError{StatusCode: 502, ResponseBody: []byte(body), RetryableOnSameAccount: sameAccount, ForceCacheBilling: true}
				require.Equal(t, execution.RecoveryExhausted, state.HandleFailoverError(ctx, unscheduler, owner.ID, service.PlatformOpenAI, 5, failure))
				require.Empty(t, state.SameAccountRetryCount)
				require.Zero(t, state.SwitchCount)
				require.Empty(t, state.FailedAccountIDs)
				require.Empty(t, unscheduler.calls)
				require.False(t, state.ForceCacheBilling)
				require.Same(t, failure, state.LastFailoverErr)
				require.Equal(t, execution.RecoveryExhausted, state.HandleSelectionExhausted(ctx, service.ErrNoAvailableAccounts))
				require.Equal(t, owner.ID, service.TurnRetryAccount(ctx).ID)
				require.Zero(t, state.SwitchCount)
			}
		})
	}
}

func TestFailoverCommittedOutputCannotReplayWithRemainingBudget(t *testing.T) {
	ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
	owner := &service.Account{ID: 42}
	service.PinTurnRetryAccount(ctx, owner)
	require.True(t, service.OpenAITurnRetryAllowed(ctx, owner.ID, nil))
	service.MarkTurnOutputCommitted(ctx)
	require.False(t, service.OpenAITurnRetryAllowed(ctx, owner.ID, nil), "the shared retry gate also fences committed output")
	require.True(t, service.TurnOutputCommitted(ctx))
	state := NewFailoverState(5, true)
	unscheduler := &mockTempUnscheduler{}
	failure := &service.UpstreamFailoverError{StatusCode: 503, RetryableOnSameAccount: true, SafeToFailoverAfterWrite: true, RequestNotSent: true}
	require.Equal(t, execution.RecoveryExhausted, state.HandleFailoverError(ctx, unscheduler, owner.ID, service.PlatformOpenAI, 5, failure))
	require.Equal(t, execution.RecoveryExhausted, state.HandleSelectionExhausted(ctx, service.ErrNoAvailableAccounts))
	require.Empty(t, state.SameAccountRetryCount)
	require.Empty(t, state.FailedAccountIDs)
	require.Empty(t, unscheduler.calls)
	require.Zero(t, state.SwitchCount)
	require.False(t, state.ForceCacheBilling)
	require.Equal(t, owner.ID, service.TurnRetryAccount(ctx).ID)
}

func TestFailoverSilentEmptyStreamRemainsRecoverable(t *testing.T) {
	ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
	state := NewFailoverState(5, false)
	failure := &service.UpstreamFailoverError{StatusCode: 502, ResponseBody: []byte(`{"error":{"type":"upstream_error","code":"openai_silent_refusal"}}`)}
	unscheduler := &mockTempUnscheduler{}
	require.Equal(t, execution.RecoveryContinue, state.HandleFailoverError(ctx, unscheduler, 43, service.PlatformOpenAI, 1, failure))
	require.Equal(t, 1, state.SwitchCount)
	require.Contains(t, state.FailedAccountIDs, int64(43))
	require.Empty(t, unscheduler.calls)
}

func TestFailoverSelectionCannotReopenExhaustedTurnBudget(t *testing.T) {
	ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
	service.PinTurnRetryAccount(ctx, &service.Account{ID: 44})
	for range 8 {
		require.NoError(t, service.TakeOpenAITurnAttempt(ctx))
	}
	state := NewFailoverState(5, false)
	state.LastFailoverErr = &service.UpstreamFailoverError{StatusCode: 503}
	require.Equal(t, execution.RecoveryExhausted, state.HandleSelectionExhausted(ctx, service.ErrNoAvailableAccounts))
	require.Zero(t, state.SwitchCount)
	require.Empty(t, state.FailedAccountIDs)
}

func TestSameAccountRetryDelayFor(t *testing.T) {
	capacityErr := &service.UpstreamFailoverError{RequestScopedTransient: true}

	for _, tc := range []struct {
		name       string
		retryCount int
		want       time.Duration
	}{
		{name: "first retry", retryCount: 1, want: 500 * time.Millisecond},
		{name: "second retry", retryCount: 2, want: time.Second},
		{name: "third retry", retryCount: 3, want: 2 * time.Second},
		{name: "fourth retry", retryCount: 4, want: 4 * time.Second},
		{name: "fifth retry", retryCount: 5, want: 8 * time.Second},
		{name: "capped retry", retryCount: 10, want: 8 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sameAccountRetryDelayFor(capacityErr, tc.retryCount))
		})
	}

	t.Run("non request scoped errors keep fixed delay", func(t *testing.T) {
		require.Equal(t, 500*time.Millisecond, sameAccountRetryDelayFor(&service.UpstreamFailoverError{}, 10))
	})

	t.Run("nil error keeps fixed delay", func(t *testing.T) {
		require.Equal(t, 500*time.Millisecond, sameAccountRetryDelayFor(nil, 10))
	})

	t.Run("explicit oauth delay wins", func(t *testing.T) {
		err := &service.UpstreamFailoverError{SameAccountRetryDelay: 3 * time.Second}
		require.Equal(t, 3*time.Second, sameAccountRetryDelayFor(err, 1))
	})
}

func TestSameAccountRetryDeadlineCannotExtendCountBudget(t *testing.T) {
	err := &service.UpstreamFailoverError{
		RetryableOnSameAccount:   true,
		SameAccountRetryDeadline: time.Now().Add(time.Minute),
	}
	require.False(t, sameAccountRetryAllowed(err, 100, 0))
	require.False(t, sameAccountRetryAllowed(err, 100, execution.MaxSameAccountRetries))
	err.SameAccountRetryDeadline = time.Now().Add(-time.Second)
	require.False(t, sameAccountRetryAllowed(err, 0, 100))
}

func TestSameAccountRetryAllowedRequiresOptInAndDefaultsToCountLimit(t *testing.T) {
	err := &service.UpstreamFailoverError{SameAccountRetryDeadline: time.Now().Add(time.Minute)}
	require.False(t, sameAccountRetryAllowed(err, 0, execution.MaxSameAccountRetries))

	err.RetryableOnSameAccount = true
	err.SameAccountRetryDeadline = time.Time{}
	require.True(t, sameAccountRetryAllowed(err, execution.MaxSameAccountRetries-1, execution.MaxSameAccountRetries))
	require.False(t, sameAccountRetryAllowed(err, execution.MaxSameAccountRetries, execution.MaxSameAccountRetries))
}

func TestSameAccountRetryAllowedHonorsErrorMaxBeforeDeadline(t *testing.T) {
	err := &service.UpstreamFailoverError{
		RetryableOnSameAccount:   true,
		SameAccountRetryDeadline: time.Now().Add(time.Minute),
		SameAccountRetryMax:      1,
	}
	require.True(t, sameAccountRetryAllowed(err, 0, execution.MaxSameAccountRetries))
	require.False(t, sameAccountRetryAllowed(err, 1, execution.MaxSameAccountRetries))
	require.False(t, sameAccountRetryAllowed(err, 0, 0), "an explicit zero retry budget remains disabled")
}

func TestEffectiveSameAccountRetryLimitHonorsErrorCapAndDisabledAccount(t *testing.T) {
	account := &service.Account{Type: service.AccountTypeAPIKey, Credentials: map[string]any{"pool_mode": true, "pool_mode_retry_count": float64(3)}}
	require.Equal(t, 1, effectiveSameAccountRetryLimit(&service.UpstreamFailoverError{SameAccountRetryMax: 1}, account))
	account.Credentials["pool_mode_retry_count"] = float64(0)
	require.Equal(t, 0, effectiveSameAccountRetryLimit(&service.UpstreamFailoverError{SameAccountRetryMax: 1}, account))
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func newTestFailoverErr(statusCode int, retryable, forceBilling bool) *service.UpstreamFailoverError {
	return &service.UpstreamFailoverError{
		StatusCode:             statusCode,
		RetryableOnSameAccount: retryable,
		ForceCacheBilling:      forceBilling,
	}
}

// ---------------------------------------------------------------------------
// NewFailoverState 测试
// ---------------------------------------------------------------------------

func TestNewFailoverState(t *testing.T) {
	t.Run("初始化字段正确", func(t *testing.T) {
		fs := NewFailoverState(5, true)
		require.Equal(t, 5, fs.MaxSwitches)
		require.Equal(t, 0, fs.SwitchCount)
		require.NotNil(t, fs.FailedAccountIDs)
		require.Empty(t, fs.FailedAccountIDs)
		require.NotNil(t, fs.SameAccountRetryCount)
		require.Empty(t, fs.SameAccountRetryCount)
		require.Nil(t, fs.LastFailoverErr)
		require.False(t, fs.ForceCacheBilling)
		require.True(t, fs.HasBoundSession())
	})

	t.Run("无绑定会话", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		require.Equal(t, 3, fs.MaxSwitches)
		require.False(t, fs.HasBoundSession())
	})

	t.Run("零最大切换次数", func(t *testing.T) {
		fs := NewFailoverState(0, false)
		require.Equal(t, 0, fs.MaxSwitches)
	})
}

// ---------------------------------------------------------------------------
// sleepWithContext 测试
// ---------------------------------------------------------------------------

func TestSleepWithContext(t *testing.T) {
	t.Run("零时长立即返回true", func(t *testing.T) {
		start := time.Now()
		ok := sleepWithContext(context.Background(), 0)
		require.True(t, ok)
		require.Less(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("负时长立即返回true", func(t *testing.T) {
		start := time.Now()
		ok := sleepWithContext(context.Background(), -1*time.Second)
		require.True(t, ok)
		require.Less(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("正常等待后返回true", func(t *testing.T) {
		start := time.Now()
		ok := sleepWithContext(context.Background(), 50*time.Millisecond)
		elapsed := time.Since(start)
		require.True(t, ok)
		require.GreaterOrEqual(t, elapsed, 40*time.Millisecond)
		require.Less(t, elapsed, 500*time.Millisecond)
	})

	t.Run("已取消context立即返回false", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		ok := sleepWithContext(ctx, 5*time.Second)
		require.False(t, ok)
		require.Less(t, time.Since(start), 50*time.Millisecond)
	})

	t.Run("等待期间context取消返回false", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		ok := sleepWithContext(ctx, 5*time.Second)
		elapsed := time.Since(start)
		require.False(t, ok)
		require.Less(t, elapsed, 500*time.Millisecond)
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — 基本切换流程
// ---------------------------------------------------------------------------

func TestHandleFailoverError_BasicSwitch(t *testing.T) {
	t.Run("显式停止不切换账号且旧错误默认仍切换", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		stopErr := &service.UpstreamFailoverError{
			Stage:             service.GatewayFailureStageAccountAuth,
			Scope:             service.GatewayFailureScopeProvider,
			NextAccountAction: service.NextAccountStop,
		}

		action := fs.HandleFailoverError(context.Background(), mock, 100, service.PlatformGrok, execution.MaxSameAccountRetries, stopErr)

		require.Equal(t, execution.RecoveryExhausted, action)
		require.Zero(t, fs.SwitchCount)
		require.Empty(t, fs.FailedAccountIDs)
		require.Equal(t, stopErr, fs.LastFailoverErr)

		legacyErr := newTestFailoverErr(http.StatusTooManyRequests, false, false)
		action = fs.HandleFailoverError(context.Background(), mock, 100, service.PlatformGrok, execution.MaxSameAccountRetries, legacyErr)

		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
	})

	t.Run("已取消的认证失败不改变切换状态", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := &service.UpstreamFailoverError{
			Stage:             service.GatewayFailureStageAccountAuth,
			Scope:             service.GatewayFailureScopeAccount,
			NextAccountAction: service.NextAccountRetry,
		}

		action := fs.HandleFailoverError(ctx, mock, 101, service.PlatformGrok, execution.MaxSameAccountRetries, err)

		require.Equal(t, execution.RecoveryCanceled, action)
		require.Zero(t, fs.SwitchCount)
		require.Empty(t, fs.FailedAccountIDs)
		require.Nil(t, fs.LastFailoverErr)
		require.Empty(t, mock.calls)
	})

	t.Run("非重试错误_非Antigravity_直接切换", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)

		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
		require.Equal(t, err, fs.LastFailoverErr)
		require.False(t, fs.ForceCacheBilling)
		require.Empty(t, mock.calls, "不应调用 TempUnschedule")
	})

	t.Run("非重试错误_Antigravity_第一次切换无延迟", func(t *testing.T) {
		// switchCount 从 0→1 时，sleepFailoverDelay(ctx, 1) 的延时 = (1-1)*1s = 0
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, false)

		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 100, service.PlatformAntigravity, execution.MaxSameAccountRetries, err)
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.Less(t, elapsed, 200*time.Millisecond, "第一次切换延迟应为 0")
	})

	t.Run("非重试错误_Antigravity_第二次切换有1秒延迟", func(t *testing.T) {
		// switchCount 从 1→2 时，sleepFailoverDelay(ctx, 2) 的延时 = (2-1)*1s = 1s
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		fs.SwitchCount = 1 // 模拟已切换一次

		err := newTestFailoverErr(500, false, false)
		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 200, service.PlatformAntigravity, execution.MaxSameAccountRetries, err)
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 2, fs.SwitchCount)
		require.GreaterOrEqual(t, elapsed, 800*time.Millisecond, "第二次切换延迟应约 1s")
		require.Less(t, elapsed, 3*time.Second)
	})

	t.Run("连续切换直到耗尽", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(2, false)

		// 第一次切换：0→1
		err1 := newTestFailoverErr(500, false, false)
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err1)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SwitchCount)

		// 第二次切换：1→2
		err2 := newTestFailoverErr(502, false, false)
		action = fs.HandleFailoverError(context.Background(), mock, 200, "openai", execution.MaxSameAccountRetries, err2)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 2, fs.SwitchCount)

		// 第三次已耗尽：SwitchCount(2) >= MaxSwitches(2)
		err3 := newTestFailoverErr(503, false, false)
		action = fs.HandleFailoverError(context.Background(), mock, 300, "openai", execution.MaxSameAccountRetries, err3)
		require.Equal(t, execution.RecoveryExhausted, action)
		require.Equal(t, 2, fs.SwitchCount, "耗尽时不应继续递增")

		// 验证失败账号列表
		require.Len(t, fs.FailedAccountIDs, 3)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
		require.Contains(t, fs.FailedAccountIDs, int64(200))
		require.Contains(t, fs.FailedAccountIDs, int64(300))

		// LastFailoverErr 应为最后一次的错误
		require.Equal(t, err3, fs.LastFailoverErr)
	})

	t.Run("MaxSwitches为0时首次即耗尽", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(0, false)
		err := newTestFailoverErr(500, false, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryExhausted, action)
		require.Equal(t, 0, fs.SwitchCount)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — 缓存计费 (ForceCacheBilling)
// ---------------------------------------------------------------------------

func TestHandleFailoverError_CacheBilling(t *testing.T) {
	t.Run("hasBoundSession为true且实际切换时设置ForceCacheBilling", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, true) // hasBoundSession=true
		err := newTestFailoverErr(500, false, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.True(t, fs.ForceCacheBilling)
	})

	t.Run("同账号重试时仅凭hasBoundSession不设置ForceCacheBilling", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, true)
		err := newTestFailoverErr(400, true, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)

		require.False(t, fs.ForceCacheBilling)
		require.Zero(t, fs.SwitchCount)
	})

	t.Run("OAuth deadline不能突破同目标预算", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, true)
		fs.SameAccountRetryCount[100] = execution.MaxSameAccountRetries
		err := newTestFailoverErr(http.StatusTooManyRequests, true, false)
		err.SameAccountRetryDeadline = time.Now().Add(time.Minute)
		err.SameAccountRetryDelay = time.Nanosecond

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)

		require.True(t, fs.ForceCacheBilling)
		require.Equal(t, 1, fs.SwitchCount)
		require.Equal(t, execution.MaxSameAccountRetries, fs.SameAccountRetryCount[100])
		require.Len(t, mock.calls, 1)
	})

	t.Run("同账号重试耗尽并实际切换时设置ForceCacheBilling", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, true)
		err := newTestFailoverErr(400, true, false)

		for i := 0; i < execution.MaxSameAccountRetries; i++ {
			fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
			require.False(t, fs.ForceCacheBilling)
		}
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)

		require.True(t, fs.ForceCacheBilling)
		require.Equal(t, 1, fs.SwitchCount)
	})

	t.Run("failoverErr.ForceCacheBilling为true时设置", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, true) // ForceCacheBilling=true

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.True(t, fs.ForceCacheBilling)
	})

	t.Run("同账号重试保留显式ForceCacheBilling", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, true)
		err := newTestFailoverErr(400, true, true)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)

		require.True(t, fs.ForceCacheBilling)
		require.Zero(t, fs.SwitchCount)
	})

	t.Run("两者均为false时不设置", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.False(t, fs.ForceCacheBilling)
	})

	t.Run("一旦设置不会被后续错误重置", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		// 第一次：ForceCacheBilling=true → 设置
		err1 := newTestFailoverErr(500, false, true)
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err1)
		require.True(t, fs.ForceCacheBilling)

		// 第二次：ForceCacheBilling=false → 仍然保持 true
		err2 := newTestFailoverErr(502, false, false)
		fs.HandleFailoverError(context.Background(), mock, 200, "openai", execution.MaxSameAccountRetries, err2)
		require.True(t, fs.ForceCacheBilling, "ForceCacheBilling 一旦设置不应被重置")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — 同账号重试 (RetryableOnSameAccount)
// ---------------------------------------------------------------------------

func TestHandleFailoverError_SameAccountRetry(t *testing.T) {
	t.Run("第一次重试返回FailoverContinue", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(400, true, false)

		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[100])
		require.Equal(t, 0, fs.SwitchCount, "同账号重试不应增加切换计数")
		require.NotContains(t, fs.FailedAccountIDs, int64(100), "同账号重试不应加入失败列表")
		require.Empty(t, mock.calls, "同账号重试期间不应调用 TempUnschedule")
		// 验证等待了 sameAccountRetryDelay (500ms)
		require.GreaterOrEqual(t, elapsed, 400*time.Millisecond)
		require.Less(t, elapsed, 2*time.Second)
	})

	t.Run("达到最大重试次数前均返回FailoverContinue", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(400, true, false)

		for i := 1; i <= execution.MaxSameAccountRetries; i++ {
			action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
			require.Equal(t, execution.RecoveryContinue, action)
			require.Equal(t, i, fs.SameAccountRetryCount[100])
		}

		require.Empty(t, mock.calls, "达到最大重试次数前均不应调用 TempUnschedule")
	})

	t.Run("超过最大重试次数后触发TempUnschedule并切换", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(400, true, false)

		for i := 0; i < execution.MaxSameAccountRetries; i++ {
			fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		}
		require.Equal(t, execution.MaxSameAccountRetries, fs.SameAccountRetryCount[100])

		// 第 maxSameAccountRetries+1 次：重试耗尽，应切换账号
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.Contains(t, fs.FailedAccountIDs, int64(100))

		// 验证 TempUnschedule 被调用
		require.Len(t, mock.calls, 1)
		require.Equal(t, int64(100), mock.calls[0].accountID)
		require.Equal(t, err, mock.calls[0].failoverErr)
	})

	t.Run("不同账号独立跟踪重试次数", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(5, false)
		err := newTestFailoverErr(400, true, false)

		// 账号 100 第一次重试
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[100])

		// 账号 200 第一次重试（独立计数）
		action = fs.HandleFailoverError(context.Background(), mock, 200, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[200])
		require.Equal(t, 1, fs.SameAccountRetryCount[100], "账号 100 的计数不应受影响")
	})

	t.Run("重试耗尽后再次遇到同账号_直接切换", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(5, false)
		err := newTestFailoverErr(400, true, false)

		// 耗尽账号 100 的重试
		for i := 0; i < execution.MaxSameAccountRetries; i++ {
			fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		}
		// 第 maxSameAccountRetries+1 次: 重试耗尽 → 切换
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryContinue, action)

		// 再次遇到账号 100，计数仍为 maxSameAccountRetries，条件不满足 → 直接切换
		action = fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Len(t, mock.calls, 2, "第二次耗尽也应调用 TempUnschedule")
	})

	t.Run("尊重账号级retryLimit_配置1次只重试1次", func(t *testing.T) {
		// 回归测试：Anthropic 等路径此前硬编码同账号重试 3 次，忽略账号
		// pool_mode_retry_count 配置。此处验证传入 retryLimit=1 时只重试 1 次即切换。
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(5, false)
		err := newTestFailoverErr(403, true, false)
		const retryLimit = 1

		// 第 1 次：同账号重试
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", retryLimit, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[100])
		require.Equal(t, 0, fs.SwitchCount, "首次重试不应切换账号")
		require.Empty(t, mock.calls, "未耗尽前不应 TempUnschedule")

		// 第 2 次：已达上限 1 → 不再同账号重试，直接切换 + TempUnschedule
		action = fs.HandleFailoverError(context.Background(), mock, 100, "openai", retryLimit, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[100], "重试计数不应超过 retryLimit")
		require.Equal(t, 1, fs.SwitchCount, "重试耗尽应切换账号")
		require.Contains(t, fs.FailedAccountIDs, int64(100))
		require.Len(t, mock.calls, 1, "重试耗尽应触发 TempUnschedule")
	})

	t.Run("retryLimit为0时立即切换不重试", func(t *testing.T) {
		// pool_mode_retry_count=0 表示关闭同账号重试（如 GPT Image 账号）。
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(5, false)
		err := newTestFailoverErr(403, true, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", 0, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 0, fs.SameAccountRetryCount[100], "retryLimit=0 不应发生同账号重试")
		require.Equal(t, 1, fs.SwitchCount, "应立即切换账号")
		require.Len(t, mock.calls, 1, "应立即 TempUnschedule")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — TempUnschedule 调用验证
// ---------------------------------------------------------------------------

func TestHandleFailoverError_TempUnschedule(t *testing.T) {
	t.Run("非重试错误不调用TempUnschedule", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, false, false) // RetryableOnSameAccount=false

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.Empty(t, mock.calls)
	})

	t.Run("重试错误耗尽后调用TempUnschedule_传入正确参数", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(502, true, false)

		for i := 0; i < execution.MaxSameAccountRetries; i++ {
			fs.HandleFailoverError(context.Background(), mock, 42, "openai", execution.MaxSameAccountRetries, err)
		}
		// 再次触发时才会执行 TempUnschedule + 切换
		fs.HandleFailoverError(context.Background(), mock, 42, "openai", execution.MaxSameAccountRetries, err)

		require.Len(t, mock.calls, 1)
		require.Equal(t, int64(42), mock.calls[0].accountID)
		require.Equal(t, 502, mock.calls[0].failoverErr.StatusCode)
		require.True(t, mock.calls[0].failoverErr.RetryableOnSameAccount)
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — Context 取消
// ---------------------------------------------------------------------------

func TestHandleFailoverError_ContextCanceled(t *testing.T) {
	t.Run("同账号重试sleep期间context取消", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(400, true, false)

		ctx, cancel := context.WithCancel(service.WithSchedulingTrace(context.Background(), nil))
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel() // 通过入口检查后、sleep 期间取消
		}()

		start := time.Now()
		action := fs.HandleFailoverError(ctx, mock, 100, "openai", execution.MaxSameAccountRetries, err)
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryCanceled, action)
		require.Less(t, elapsed, 400*time.Millisecond, "sleep 应被取消打断")
		// 进入重试分支后才取消：重试计数已递增
		require.Equal(t, 1, fs.SameAccountRetryCount[100])

		trace := service.WithOpsSchedulingTrace(ctx, nil)
		require.Len(t, trace, 1)
		require.Contains(t, trace[0].Detail, `"reason":"client_canceled"`)
		require.Nil(t, service.LastOpsUpstreamAttempt(trace))
	})

	t.Run("入口即已取消_不改动任何failover状态", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(520, false, false)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // 调用前客户端已断开

		start := time.Now()
		action := fs.HandleFailoverError(ctx, mock, 100, "openai", execution.MaxSameAccountRetries, err)
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryCanceled, action)
		require.Less(t, elapsed, 100*time.Millisecond, "应立即返回")
		// 入口已取消时不得改变任何 failover 状态。
		require.Equal(t, 0, fs.SwitchCount, "取消的请求不应计入切换")
		require.Equal(t, 0, fs.SameAccountRetryCount[100], "取消的请求不应改动重试计数")
		require.NotContains(t, fs.FailedAccountIDs, int64(100))
		require.Nil(t, fs.LastFailoverErr)
		require.Empty(t, mock.calls, "不应触发 TempUnschedule")
	})

	t.Run("Antigravity延迟期间context取消", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		fs.SwitchCount = 1 // 下一次 switchCount=2 → delay = 1s
		err := newTestFailoverErr(500, false, false)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // 立即取消

		start := time.Now()
		action := fs.HandleFailoverError(ctx, mock, 100, service.PlatformAntigravity, execution.MaxSameAccountRetries, err)
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryCanceled, action)
		require.Less(t, elapsed, 100*time.Millisecond, "应立即返回而非等待 1s")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — FailedAccountIDs 跟踪
// ---------------------------------------------------------------------------

func TestHandleFailoverError_FailedAccountIDs(t *testing.T) {
	t.Run("切换时添加到失败列表", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, newTestFailoverErr(500, false, false))
		require.Contains(t, fs.FailedAccountIDs, int64(100))

		fs.HandleFailoverError(context.Background(), mock, 200, "openai", execution.MaxSameAccountRetries, newTestFailoverErr(502, false, false))
		require.Contains(t, fs.FailedAccountIDs, int64(200))
		require.Len(t, fs.FailedAccountIDs, 2)
	})

	t.Run("耗尽时也添加到失败列表", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(0, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, newTestFailoverErr(500, false, false))
		require.Equal(t, execution.RecoveryExhausted, action)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
	})

	t.Run("同账号重试期间不添加到失败列表", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, newTestFailoverErr(400, true, false))
		require.Equal(t, execution.RecoveryContinue, action)
		require.NotContains(t, fs.FailedAccountIDs, int64(100))
	})

	t.Run("同一账号多次切换不重复添加", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(5, false)

		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, newTestFailoverErr(500, false, false))
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, newTestFailoverErr(500, false, false))
		require.Len(t, fs.FailedAccountIDs, 1, "map 天然去重")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — LastFailoverErr 更新
// ---------------------------------------------------------------------------

func TestHandleFailoverError_LastFailoverErr(t *testing.T) {
	t.Run("每次调用都更新LastFailoverErr", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		err1 := newTestFailoverErr(500, false, false)
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err1)
		require.Equal(t, err1, fs.LastFailoverErr)

		err2 := newTestFailoverErr(502, false, false)
		fs.HandleFailoverError(context.Background(), mock, 200, "openai", execution.MaxSameAccountRetries, err2)
		require.Equal(t, err2, fs.LastFailoverErr)
	})

	t.Run("同账号重试时也更新LastFailoverErr", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)

		err := newTestFailoverErr(400, true, false)
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, err, fs.LastFailoverErr)
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — 综合集成场景
// ---------------------------------------------------------------------------

func TestHandleFailoverError_IntegrationScenario(t *testing.T) {
	t.Run("模拟完整failover流程_多账号混合重试与切换", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, true) // hasBoundSession=true

		// 1. 账号 100 遇到可重试错误，同账号重试 maxSameAccountRetries 次
		retryErr := newTestFailoverErr(400, true, false)
		for i := 0; i < execution.MaxSameAccountRetries; i++ {
			action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, retryErr)
			require.Equal(t, execution.RecoveryContinue, action)
			require.False(t, fs.ForceCacheBilling, "同账号重试期间不应仅因绑定会话强制缓存计费")
		}

		// 2. 账号 100 超过重试上限 → TempUnschedule + 切换
		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, retryErr)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SwitchCount)
		require.True(t, fs.ForceCacheBilling, "实际切换账号时应设置 ForceCacheBilling")
		require.Len(t, mock.calls, 1)

		// 3. 账号 200 遇到不可重试错误 → 直接切换
		switchErr := newTestFailoverErr(500, false, false)
		action = fs.HandleFailoverError(context.Background(), mock, 200, "openai", execution.MaxSameAccountRetries, switchErr)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 2, fs.SwitchCount)

		// 4. 账号 300 遇到不可重试错误 → 再切换
		action = fs.HandleFailoverError(context.Background(), mock, 300, "openai", execution.MaxSameAccountRetries, switchErr)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 3, fs.SwitchCount)

		// 5. 账号 400 → 已耗尽 (SwitchCount=3 >= MaxSwitches=3)
		action = fs.HandleFailoverError(context.Background(), mock, 400, "openai", execution.MaxSameAccountRetries, switchErr)
		require.Equal(t, execution.RecoveryExhausted, action)

		// 最终状态验证
		require.Equal(t, 3, fs.SwitchCount, "耗尽时不再递增")
		require.Len(t, fs.FailedAccountIDs, 4, "4个不同账号都在失败列表中")
		require.True(t, fs.ForceCacheBilling)
		require.Len(t, mock.calls, 1, "只有账号 100 触发了 TempUnschedule")
	})

	t.Run("模拟Antigravity平台完整流程", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(2, false)

		err := newTestFailoverErr(500, false, false)

		// 第一次切换：delay = 0s
		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 100, service.PlatformAntigravity, execution.MaxSameAccountRetries, err)
		elapsed := time.Since(start)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Less(t, elapsed, 200*time.Millisecond, "第一次切换延迟为 0")

		// 第二次切换：delay = 1s
		start = time.Now()
		action = fs.HandleFailoverError(context.Background(), mock, 200, service.PlatformAntigravity, execution.MaxSameAccountRetries, err)
		elapsed = time.Since(start)
		require.Equal(t, execution.RecoveryContinue, action)
		require.GreaterOrEqual(t, elapsed, 800*time.Millisecond, "第二次切换延迟约 1s")

		// 第三次：耗尽（无延迟，因为在检查延迟之前就返回了）
		start = time.Now()
		action = fs.HandleFailoverError(context.Background(), mock, 300, service.PlatformAntigravity, execution.MaxSameAccountRetries, err)
		elapsed = time.Since(start)
		require.Equal(t, execution.RecoveryExhausted, action)
		require.Less(t, elapsed, 200*time.Millisecond, "耗尽时不应有延迟")
	})

	t.Run("ForceCacheBilling通过错误标志设置", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false) // hasBoundSession=false

		// 第一次：ForceCacheBilling=false
		err1 := newTestFailoverErr(500, false, false)
		fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err1)
		require.False(t, fs.ForceCacheBilling)

		// 第二次：ForceCacheBilling=true（Antigravity 粘性会话切换）
		err2 := newTestFailoverErr(500, false, true)
		fs.HandleFailoverError(context.Background(), mock, 200, "openai", execution.MaxSameAccountRetries, err2)
		require.True(t, fs.ForceCacheBilling, "错误标志应触发 ForceCacheBilling")

		// 第三次：ForceCacheBilling=false，但状态仍保持 true
		err3 := newTestFailoverErr(500, false, false)
		fs.HandleFailoverError(context.Background(), mock, 300, "openai", execution.MaxSameAccountRetries, err3)
		require.True(t, fs.ForceCacheBilling, "不应重置")
	})
}

// ---------------------------------------------------------------------------
// HandleFailoverError — 边界条件
// ---------------------------------------------------------------------------

func TestHandleFailoverError_EdgeCases(t *testing.T) {
	t.Run("StatusCode为0的错误也能正常处理", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(0, false, false)

		action := fs.HandleFailoverError(context.Background(), mock, 100, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryContinue, action)
	})

	t.Run("AccountID为0也能正常跟踪", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, true, false)

		action := fs.HandleFailoverError(context.Background(), mock, 0, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[0])
	})

	t.Run("负AccountID也能正常跟踪", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		err := newTestFailoverErr(500, true, false)

		action := fs.HandleFailoverError(context.Background(), mock, -1, "openai", execution.MaxSameAccountRetries, err)
		require.Equal(t, execution.RecoveryContinue, action)
		require.Equal(t, 1, fs.SameAccountRetryCount[-1])
	})

	t.Run("空平台名称不触发Antigravity延迟", func(t *testing.T) {
		mock := &mockTempUnscheduler{}
		fs := NewFailoverState(3, false)
		fs.SwitchCount = 1
		err := newTestFailoverErr(500, false, false)

		start := time.Now()
		action := fs.HandleFailoverError(context.Background(), mock, 100, "", execution.MaxSameAccountRetries, err)
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryContinue, action)
		require.Less(t, elapsed, 200*time.Millisecond, "空平台不应触发 Antigravity 延迟")
	})
}

// ---------------------------------------------------------------------------
// HandleSelectionExhausted 测试
// ---------------------------------------------------------------------------

func TestHandleSelectionExhausted(t *testing.T) {
	t.Run("无LastFailoverErr时返回Exhausted", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		// LastFailoverErr 为 nil

		action := fs.HandleSelectionExhausted(context.Background())
		require.Equal(t, execution.RecoveryExhausted, action)
	})

	t.Run("非503错误返回Exhausted", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		fs.LastFailoverErr = newTestFailoverErr(500, false, false)

		action := fs.HandleSelectionExhausted(context.Background())
		require.Equal(t, execution.RecoveryExhausted, action)
	})

	t.Run("503候选耗尽_保留排除列表并停止", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		fs.LastFailoverErr = newTestFailoverErr(503, false, false)
		fs.FailedAccountIDs[100] = struct{}{}
		fs.SwitchCount = 1

		start := time.Now()
		action := fs.HandleSelectionExhausted(context.Background())
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryExhausted, action)
		require.Contains(t, fs.FailedAccountIDs, int64(100))
		require.Less(t, elapsed, 100*time.Millisecond)
	})

	t.Run("503但SwitchCount已超过MaxSwitches_返回Exhausted", func(t *testing.T) {
		fs := NewFailoverState(2, false)
		fs.LastFailoverErr = newTestFailoverErr(503, false, false)
		fs.SwitchCount = 3 // > MaxSwitches(2)

		start := time.Now()
		action := fs.HandleSelectionExhausted(context.Background())
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryExhausted, action)
		require.Less(t, elapsed, 100*time.Millisecond, "不应等待")
	})

	t.Run("503但context已取消_返回Canceled", func(t *testing.T) {
		fs := NewFailoverState(3, false)
		fs.LastFailoverErr = newTestFailoverErr(503, false, false)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		action := fs.HandleSelectionExhausted(ctx)
		elapsed := time.Since(start)

		require.Equal(t, execution.RecoveryCanceled, action)
		require.Less(t, elapsed, 100*time.Millisecond, "应立即返回")
	})

	t.Run("context已取消_非503也返回Canceled而非Exhausted", func(t *testing.T) {
		// #4257 核心场景：客户端断开后选号失败源于 context canceled，
		// 不应被当成账号耗尽转成 502。
		fs := NewFailoverState(3, false)
		fs.LastFailoverErr = newTestFailoverErr(520, false, false)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		action := fs.HandleSelectionExhausted(ctx)
		require.Equal(t, execution.RecoveryCanceled, action)
	})

	t.Run("context已取消_无LastFailoverErr也返回Canceled", func(t *testing.T) {
		fs := NewFailoverState(3, false)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		action := fs.HandleSelectionExhausted(ctx)
		require.Equal(t, execution.RecoveryCanceled, action)
	})

	t.Run("503且SwitchCount等于MaxSwitches_停止", func(t *testing.T) {
		fs := NewFailoverState(2, false)
		fs.LastFailoverErr = newTestFailoverErr(503, false, false)
		fs.SwitchCount = 2 // == MaxSwitches，条件是 <=，停止

		action := fs.HandleSelectionExhausted(context.Background())
		require.Equal(t, execution.RecoveryExhausted, action)
	})
}

// ---------------------------------------------------------------------------
// failoverClientGone 测试
// ---------------------------------------------------------------------------

func TestFailoverClientGone(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("活跃请求返回false", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

		require.False(t, failoverClientGone(c))
		require.Equal(t, http.StatusOK, c.Writer.Status(), "不应改动状态码")
	})

	t.Run("客户端已断开_返回true并标记499", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)

		require.True(t, failoverClientGone(c))
		require.Equal(t, statusClientClosedRequest, c.Writer.Status())
	})

	t.Run("响应已提交_不改状态码", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
		c.String(http.StatusOK, "partial")

		require.True(t, failoverClientGone(c))
		require.Equal(t, http.StatusOK, c.Writer.Status(), "已提交的状态码不应被覆盖")
	})

	t.Run("nil安全", func(t *testing.T) {
		require.False(t, failoverClientGone(nil))
	})
}

type openAIRecoverySchedulerStub struct {
	switches int
	stop429  bool
}

func (s *openAIRecoverySchedulerStub) RecordOpenAIAccountSwitch() { s.switches++ }
func (s *openAIRecoverySchedulerStub) ShouldStopOpenAIOAuth429Failover(_ *service.Account, status, _ int, _ *service.OpenAIOAuth429FailoverState) bool {
	return s.stop429 && status == 429
}

func TestOpenAIRecoveryController(t *testing.T) {
	account := &service.Account{ID: 12, Extra: map[string]any{"pool_mode": true, "pool_mode_retry_count": 3}}
	failure := func() *service.UpstreamFailoverError {
		return &service.UpstreamFailoverError{StatusCode: 502, SameAccountRetryDelay: time.Nanosecond}
	}
	t.Run("upstream_delay_must_fit_existing_budgets", func(t *testing.T) {
		for _, test := range []struct {
			name       string
			retryAfter string
			delay      time.Duration
			window     time.Duration
			wantErr    error
			wantCount  int
		}{
			{name: "total wait budget", retryAfter: "30", delay: 30 * time.Second, wantErr: service.ErrTurnRetryWaitExhausted, wantCount: 1},
			{name: "account retry deadline", retryAfter: "7", delay: 7 * time.Second, window: 5 * time.Second},
		} {
			t.Run(test.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(service.EnsureOpenAITurnBudget(t.Context(), nil), 200*time.Millisecond)
				defer cancel()
				state := NewFailoverState(3, false)
				scheduler := &openAIRecoverySchedulerStub{}
				fact := &service.UpstreamFailoverError{
					StatusCode:             http.StatusServiceUnavailable,
					RetryableOnSameAccount: true,
					RequestScopedTransient: true,
					SameAccountRetryDelay:  test.delay,
					ResponseBody:           []byte(`{"error":{"code":"server_is_overloaded"}}`),
					ResponseHeaders:        http.Header{"Retry-After": []string{test.retryAfter}},
				}
				if test.window > 0 {
					fact.SameAccountRetryDeadline = time.Now().Add(test.window)
				}
				action, err := state.RecoverOpenAI(ctx, nil, account, fact, false, true, scheduler, nil)
				if test.wantErr != nil {
					require.ErrorIs(t, err, test.wantErr)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, execution.RecoveryExhausted, action, "no upstream attempt may start early")
				require.Equal(t, test.wantCount, state.SameAccountRetryCount[account.ID])
				require.Zero(t, scheduler.switches)
				require.Empty(t, state.FailedAccountIDs)
				require.Same(t, fact, state.LastFailoverErr)
				require.Equal(t, test.retryAfter, fact.ResponseHeaders.Get("Retry-After"))
				require.JSONEq(t, `{"error":{"code":"server_is_overloaded"}}`, string(fact.ResponseBody))
			})
		}
	})
	t.Run("one_same_owner_then_switch_then_stop", func(t *testing.T) {
		ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
		scheduler := &openAIRecoverySchedulerStub{}
		state := NewFailoverState(1, false)
		for _, want := range []execution.RecoveryAction{execution.RecoveryRetrySameAccount, execution.RecoveryContinue, execution.RecoveryExhausted} {
			action, err := state.RecoverOpenAI(ctx, nil, account, failure(), true, true, scheduler, nil)
			require.NoError(t, err)
			require.Equal(t, want, action)
		}
		require.Equal(t, 1, state.SameAccountRetryCount[account.ID])
		require.Equal(t, 1, state.SwitchCount)
		require.Equal(t, 1, scheduler.switches)
		require.Contains(t, state.FailedAccountIDs, account.ID)
		require.Nil(t, service.TurnRetryAccount(ctx))
	})
	t.Run("owner_bound_context_does_not_move", func(t *testing.T) {
		ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
		state := NewFailoverState(3, false)
		scheduler := &openAIRecoverySchedulerStub{}
		for _, want := range []execution.RecoveryAction{execution.RecoveryRetrySameAccount, execution.RecoveryExhausted} {
			action, err := state.RecoverOpenAI(ctx, nil, account, failure(), false, true, scheduler, nil)
			require.NoError(t, err)
			require.Equal(t, want, action)
		}
		require.Zero(t, scheduler.switches)
		require.Empty(t, state.FailedAccountIDs)
	})
	t.Run("cancel_output_attempt_and_preparation_limits", func(t *testing.T) {
		for _, mode := range []string{"cancel", "output", "attempts", "preparation", "terminal"} {
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(service.EnsureOpenAITurnBudget(t.Context(), nil))
				defer cancel()
				errFact := failure()
				want := execution.RecoveryExhausted
				switch mode {
				case "cancel":
					cancel()
					want = execution.RecoveryCanceled
				case "output":
					service.MarkTurnOutputCommitted(ctx)
				case "attempts":
					for range 8 {
						require.NoError(t, service.TakeOpenAITurnAttempt(ctx))
					}
				case "preparation":
					require.NoError(t, service.TakeTurnPreparationRetry(ctx))
					require.NoError(t, service.TakeTurnPreparationRetry(ctx))
					errFact.Stage = service.GatewayFailureStageAccountAuth
					errFact.RequestNotSent = true
				case "terminal":
					errFact.NextAccountAction = service.NextAccountStop
				}
				state := NewFailoverState(3, false)
				scheduler := &openAIRecoverySchedulerStub{}
				action, err := state.RecoverOpenAI(ctx, nil, account, errFact, true, true, scheduler, nil)
				require.Equal(t, want, action)
				if mode == "preparation" {
					require.ErrorIs(t, err, service.ErrTurnPreparationExhausted)
				}
				require.Zero(t, scheduler.switches)
				require.Empty(t, state.SameAccountRetryCount)
				require.Empty(t, state.FailedAccountIDs)
			})
		}
	})
	t.Run("oauth_429_cap_and_bps_exception", func(t *testing.T) {
		for _, bps := range []bool{false, true} {
			state := NewFailoverState(3, false)
			scheduler := &openAIRecoverySchedulerStub{stop429: true}
			fact := &service.UpstreamFailoverError{StatusCode: 429}
			want := execution.RecoveryExhausted
			if bps {
				fact.Reason = service.ExcelBPSRateLimitedReason
				want = execution.RecoveryContinue
			}
			action, err := state.RecoverOpenAI(service.EnsureOpenAITurnBudget(t.Context(), nil), nil, account, fact, true, false, scheduler, nil)
			require.NoError(t, err)
			require.Equal(t, want, action)
		}
	})
}

func TestCloudPreInferenceFailureUsesExistingRecoveryController(t *testing.T) {
	ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
	account := &service.Account{ID: 12, Extra: map[string]any{"openai_cloud": true, "pool_mode": true, "pool_mode_retry_count": 3}}
	state := NewFailoverState(3, false)
	scheduler := &openAIRecoverySchedulerStub{}
	failure := &service.UpstreamFailoverError{StatusCode: 502, Reason: service.CloudPreInferenceFailureReason,
		RequestNotSent: true, RejectedBeforeGeneration: true, SafeToFailoverAfterWrite: true,
		RetryableOnSameAccount: true, SameAccountRetryMax: 1, SameAccountRetryDelay: time.Nanosecond}
	for _, want := range []execution.RecoveryAction{execution.RecoveryRetrySameAccount, execution.RecoveryContinue} {
		action, err := state.RecoverOpenAI(ctx, nil, account, failure, true, true, scheduler, nil)
		require.NoError(t, err)
		require.Equal(t, want, action)
	}
	require.Equal(t, 1, scheduler.switches)
	require.Contains(t, state.FailedAccountIDs, account.ID)
	require.Nil(t, service.TurnRetryAccount(ctx))
	count := 99
	require.False(t, openAIFirstOutputFailoverExhausted(failure, &count), "pre-inference failure uses the common turn limit")
	require.Equal(t, 99, count)
}

func TestFailoverExecutionOwnerSharesIngressStorage(t *testing.T) {
	state := NewFailoverState(2, true)
	require.Same(t, &state.SwitchCount, &state.Recovery.SwitchCount)
	require.Same(t, &state.ForceCacheBilling, &state.Recovery.ForceCacheBilling)
	state.SameAccountRetryCount[7] = 1
	require.Equal(t, 1, state.Recovery.SameAccountRetryCount[7])
	state.RecordProfitVeto(8)
	require.Contains(t, state.Recovery.FailedAccountIDs, int64(8))
	require.Equal(t, 1, state.Recovery.ProfitVetoCount())
	require.Zero(t, state.SwitchCount)
}

func TestFailoverExecutionPreservesPreparationFailureCause(t *testing.T) {
	ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
	require.NoError(t, service.TakeTurnPreparationRetry(ctx))
	require.NoError(t, service.TakeTurnPreparationRetry(ctx))
	state := NewFailoverState(3, true)
	failure := &service.UpstreamFailoverError{StatusCode: 503, Stage: service.GatewayFailureStageAccountAuth, RequestNotSent: true, RetryableOnSameAccount: true, ForceCacheBilling: true}
	unscheduler := &mockTempUnscheduler{}
	require.Equal(t, execution.RecoveryExhausted, state.HandleFailoverError(ctx, unscheduler, 7, service.PlatformOpenAI, 1, failure))
	require.ErrorIs(t, state.LastFailoverErr, service.ErrTurnPreparationExhausted)
	require.Equal(t, service.GatewayFailureReason("preparation_budget_exhausted"), state.LastFailoverErr.Reason)
	require.Equal(t, 503, state.LastFailoverErr.StatusCode)
	require.Empty(t, state.SameAccountRetryCount)
	require.Empty(t, state.FailedAccountIDs)
	require.Empty(t, unscheduler.calls)
	require.False(t, state.ForceCacheBilling)
	require.ErrorIs(t, service.TakeTurnPreparationRetry(ctx), service.ErrTurnPreparationExhausted, "the callback cannot replace the existing owner")
}

func TestFailoverExecutionKeepsSendEvidenceDistinct(t *testing.T) {
	for _, mode := range []string{"ambiguous", "not sent", "rejected before generation"} {
		t.Run(mode, func(t *testing.T) {
			ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
			state := NewFailoverState(3, false)
			state.UncertainReplaySafe = false
			failure := &service.UpstreamFailoverError{StatusCode: 502}
			want := execution.RecoveryContinue
			switch mode {
			case "ambiguous":
				want = execution.RecoveryExhausted
			case "not sent":
				failure.RequestNotSent = true
			case "rejected before generation":
				failure.RejectedBeforeGeneration = true
			}
			action := state.HandleFailoverError(ctx, &mockTempUnscheduler{}, 7, service.PlatformOpenAI, 0, failure)
			require.Equal(t, want, action)
			require.Equal(t, mode == "not sent", recoveryFailure(failure).RequestNotSent)
			require.Equal(t, mode == "rejected before generation", recoveryFailure(failure).RejectedBeforeGeneration)
			if mode == "ambiguous" {
				require.Empty(t, state.FailedAccountIDs)
				require.Zero(t, state.SwitchCount)
			} else {
				require.Contains(t, state.FailedAccountIDs, int64(7))
				require.Equal(t, 1, state.SwitchCount)
			}
		})
	}
}

func TestSelectionExhaustedExecutionMovesOnlyUnavailablePinnedOwner(t *testing.T) {
	for _, dependency := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty pool", true: "dependency"}[dependency], func(t *testing.T) {
			ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
			pinned := &service.Account{ID: 71}
			service.PinTurnRetryAccount(ctx, pinned)
			state := NewFailoverState(2, false)
			state.LastFailoverErr = &service.UpstreamFailoverError{StatusCode: 503}
			state.FailedAccountIDs[70] = struct{}{}
			selectionErr := service.ErrNoAvailableAccounts
			if dependency {
				selectionErr = service.ErrSelectionDependency
			}
			action := state.HandleSelectionExhausted(ctx, selectionErr)
			require.Contains(t, state.FailedAccountIDs, int64(70))
			if dependency {
				require.Equal(t, execution.RecoveryExhausted, action)
				require.Same(t, pinned, service.TurnRetryAccount(ctx))
				require.Zero(t, state.SwitchCount)
			} else {
				require.Equal(t, execution.RecoveryContinue, action)
				require.Nil(t, service.TurnRetryAccount(ctx))
				require.Contains(t, state.FailedAccountIDs, pinned.ID)
				require.Equal(t, 1, state.SwitchCount)
			}
		})
	}
}

func TestOpenAISelectionRecoveryPreservesErrorMappingAndOwner(t *testing.T) {
	for _, mode := range []string{"move", "no pin", "dependency", "compact absence", "missing last", "terminal", "nonportable", "cached nonportable", "switch limit", "canceled", "output", "budget", "refusal", "scheduler cap", "bps exception"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(service.EnsureOpenAITurnBudget(t.Context(), nil))
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			account := &service.Account{ID: 12, Platform: service.PlatformOpenAI}
			service.PinTurnRetryAccount(ctx, account)
			state := NewFailoverState(2, false)
			failure := &service.UpstreamFailoverError{StatusCode: 503}
			state.LastFailoverErr = failure
			scheduler := &openAIRecoverySchedulerStub{}
			body := []byte("{}")
			selectionErr := service.ErrNoAvailableAccounts
			wantHandled := true
			wantAction := execution.RecoveryExhausted
			switch mode {
			case "move":
				wantAction = execution.RecoveryContinue
			case "no pin":
				service.PinTurnRetryAccount(ctx, nil)
				wantHandled = false
			case "dependency":
				selectionErr = service.ErrSelectionDependency
				wantHandled = false
			case "compact absence":
				selectionErr = service.ErrNoAvailableCompactAccounts
				wantHandled = false
			case "missing last":
				state.LastFailoverErr = nil
				wantHandled = false
			case "terminal":
				failure.NextAccountAction = service.NextAccountStop
				wantHandled = false
			case "nonportable":
				body = []byte("{\"previous_response_id\":\"resp_existing\"}")
				wantHandled = false
			case "cached nonportable":
				c.Set(service.OpenAIAccountReplayAllowedContextKey, false)
				wantHandled = false
			case "switch limit":
				state.SwitchCount = 2
				wantHandled = false
			case "canceled":
				cancel()
				wantAction = execution.RecoveryCanceled
			case "output":
				service.MarkTurnOutputCommitted(ctx)
			case "budget":
				for range 8 {
					require.NoError(t, service.TakeOpenAITurnAttempt(ctx))
				}
			case "refusal":
				failure.ResponseBody = []byte("{\"stop_reason\":\"refusal\"}")
			case "scheduler cap":
				scheduler.stop429 = true
				failure.StatusCode = 429
			case "bps exception":
				scheduler.stop429 = true
				failure.StatusCode = 429
				failure.Reason = service.ExcelBPSRateLimitedReason
				wantAction = execution.RecoveryContinue
			}
			before := state.LastFailoverErr
			action, handled := state.RecoverOpenAISelection(ctx, c, selectionErr, body, scheduler, nil)
			require.Equal(t, wantHandled, handled)
			require.Equal(t, wantAction, action)
			if before == nil {
				require.Nil(t, state.LastFailoverErr)
			} else {
				require.Same(t, before, state.LastFailoverErr, "ingress maps the same original upstream failure")
			}
			if !handled || mode == "canceled" {
				require.Empty(t, state.FailedAccountIDs)
				if mode != "no pin" {
					require.Same(t, account, service.TurnRetryAccount(ctx))
				}
			} else {
				require.Contains(t, state.FailedAccountIDs, account.ID)
				require.Nil(t, service.TurnRetryAccount(ctx))
			}
			require.Empty(t, state.SameAccountRetryCount)
			if mode == "move" || mode == "bps exception" {
				require.Equal(t, 1, scheduler.switches)
			} else {
				require.Zero(t, scheduler.switches)
			}
		})
	}
}

func TestOpenAIRecoveryBeginOperationKeepsBudgetAndDuplicateCallbacks(t *testing.T) {
	ctx := service.EnsureOpenAITurnBudget(t.Context(), nil)
	for range 8 {
		require.NoError(t, service.TakeOpenAITurnAttempt(ctx))
	}
	require.NoError(t, service.TakeTurnPreparationRetry(ctx))
	require.NoError(t, service.TakeTurnPreparationRetry(ctx))
	service.MarkTurnOutputCommitted(ctx)
	state := NewFailoverState(2, false)
	original := &service.UpstreamFailoverError{StatusCode: 503}
	state.LastFailoverErr = original
	state.RecordProfitVeto(8)
	state.SameAccountRetryCount[9] = 1
	state.SwitchCount = 1
	require.False(t, state.BeginOperation(1))
	require.Same(t, original, state.LastFailoverErr)
	require.True(t, state.BeginOperation(2))
	require.Nil(t, state.LastFailoverErr)
	require.Empty(t, state.FailedAccountIDs)
	require.Empty(t, state.SameAccountRetryCount)
	require.Zero(t, state.ProfitVetoCount())
	require.Zero(t, state.SwitchCount)
	state.LastFailoverErr = original
	state.RecordProfitVeto(10)
	require.False(t, state.BeginOperation(2))
	require.Same(t, original, state.LastFailoverErr)
	require.Equal(t, 1, state.ProfitVetoCount())
	require.True(t, service.TurnOutputCommitted(ctx))
	require.False(t, service.OpenAITurnRetryAllowed(ctx, 9, nil))
	require.ErrorIs(t, service.TakeTurnPreparationRetry(ctx), service.ErrTurnPreparationExhausted)
}
