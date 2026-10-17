package handler

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TempUnscheduler 用于 HandleFailoverError 中同账号重试耗尽后的临时封禁。
// GatewayService 隐式实现此接口。
type TempUnscheduler interface {
	TempUnscheduleRetryableError(ctx context.Context, accountID int64, failoverErr *service.UpstreamFailoverError)
}

// FailoverAction 表示 failover 错误处理后的下一步动作
type FailoverAction int

const (
	// FailoverContinue 继续循环（同账号重试或切换账号，调用方统一 continue）
	FailoverContinue FailoverAction = iota
	// FailoverExhausted 切换次数耗尽（调用方应返回错误响应）
	FailoverExhausted
	// FailoverCanceled context 已取消（调用方应直接 return）
	FailoverCanceled
)

const (
	// maxSameAccountRetries 同账号重试次数默认上限（针对 RetryableOnSameAccount 错误）。
	// 生产调用方通常传入账号级配置 account.GetPoolModeRetryCount()，该常量仅作兜底/测试默认值。
	maxSameAccountRetries = 1
	// sameAccountRetryDelay 同账号重试间隔
	sameAccountRetryDelay = 500 * time.Millisecond
	// maxRequestScopedRetryDelay 限制请求级瞬时错误的指数退避上限，避免高重试配置
	// 将单次请求拖入分钟级等待。
	maxRequestScopedRetryDelay = 8 * time.Second
	// maxProfitVetoAttempts 单次请求内允许的分组利润门终检否决次数上限。
	// 利润否决不产生上游请求，因此不会推进 SwitchCount；没有独立上限的话，
	// 「选号 → 终检否决 → 重选」在候选池与账号快照短暂不一致时可以空转很久。
	// 取值与 maxAccountSwitches 默认值一致：混合定价的大分组仍有充分重选机会，
	// 同时把整池越线时的无谓选号开销限制在常数级。
	maxProfitVetoAttempts = 10
)

// profitVetoExhaustedMessage 是利润否决次数耗尽时返回给客户端的文案。
// 语义上等同于「无可用账号」：候选账号都不满足分组的利润约束。
const profitVetoExhaustedMessage = "No available accounts: all candidates rejected by group profit control"

func sameAccountRetryDelayFor(failoverErr *service.UpstreamFailoverError, retryCount int) time.Duration {
	if failoverErr == nil {
		return sameAccountRetryDelay
	}
	if failoverErr.SameAccountRetryDelay > 0 {
		return failoverErr.SameAccountRetryDelay
	}
	if !failoverErr.RequestScopedTransient || retryCount <= 1 {
		return sameAccountRetryDelay
	}

	delay := sameAccountRetryDelay
	for i := 1; i < retryCount; i++ {
		if delay >= maxRequestScopedRetryDelay/2 {
			return maxRequestScopedRetryDelay
		}
		delay *= 2
	}
	return delay
}

func sameAccountRetryAllowed(failoverErr *service.UpstreamFailoverError, retryCount, retryLimit int) bool {
	if failoverErr == nil || !failoverErr.RetryableOnSameAccount {
		return false
	}
	if !sameAccountRetryDeadlineAllows(failoverErr) {
		return false
	}
	// One repeat of the same target, including OAuth retry windows. Explicit
	// account opt-out and a stricter provider cap still win.
	if retryLimit <= 0 {
		return false
	}
	return retryCount < min(retryLimit, 1)
}

// sameAccountRetryDeadlineAllows prevents a retry from starting after the
// service-provided same-account retry window has elapsed.
func sameAccountRetryDeadlineAllows(failoverErr *service.UpstreamFailoverError) bool {
	return failoverErr == nil || failoverErr.SameAccountRetryDeadline.IsZero() || time.Now().Before(failoverErr.SameAccountRetryDeadline)
}

// effectiveSameAccountRetryLimit applies an error-specific cap without
// overriding an explicit account setting of zero (which disables retries).
func effectiveSameAccountRetryLimit(failoverErr *service.UpstreamFailoverError, account *service.Account) int {
	if account == nil {
		return 0
	}
	limit := account.GetPoolModeRetryCount()
	if limit > 0 && failoverErr != nil && failoverErr.SameAccountRetryMax > 0 && failoverErr.SameAccountRetryMax < limit {
		return failoverErr.SameAccountRetryMax
	}
	return limit
}

// FailoverState 跨循环迭代共享的 failover 状态
type FailoverState struct {
	SwitchCount           int
	MaxSwitches           int
	FailedAccountIDs      map[int64]struct{}
	SameAccountRetryCount map[int64]int
	LastFailoverErr       *service.UpstreamFailoverError
	ForceCacheBilling     bool
	ReplayAllowed         bool
	UncertainReplaySafe   bool
	hasBoundSession       bool

	// profitVetoCount 本次请求累计的利润否决次数，用于 maxProfitVetoAttempts 上限。
	profitVetoCount int
}

// NewFailoverState 创建 failover 状态
func NewFailoverState(maxSwitches int, hasBoundSession bool) *FailoverState {
	return &FailoverState{
		MaxSwitches:   maxSwitches,
		ReplayAllowed: true, UncertainReplaySafe: true,
		FailedAccountIDs:      make(map[int64]struct{}),
		SameAccountRetryCount: make(map[int64]int),
		hasBoundSession:       hasBoundSession,
	}
}

// RecordProfitVeto records a permanent request-local exclusion and bounds veto attempts.
//
// 返回 FailoverContinue 表示调用方可以继续重选下一个账号；返回 FailoverExhausted
// 表示本次请求的利润否决次数已达上限，调用方应按「无可用账号」终止，
// 不得继续 continue。
func (s *FailoverState) RecordProfitVeto(accountID int64) FailoverAction {
	s.FailedAccountIDs[accountID] = struct{}{}
	s.profitVetoCount++
	if s.profitVetoCount >= maxProfitVetoAttempts {
		return FailoverExhausted
	}
	return FailoverContinue
}

// ProfitVetoCount 返回本次请求累计的利润否决次数（供日志使用）。
func (s *FailoverState) ProfitVetoCount() int { return s.profitVetoCount }

// HandleFailoverError 处理 UpstreamFailoverError，返回下一步动作。
// 包含：缓存计费判断、同账号重试、临时封禁、切换计数、Antigravity 延时。
func (s *FailoverState) HandleFailoverError(
	ctx context.Context,
	gatewayService TempUnscheduler,
	accountID int64,
	platform string,
	retryLimit int,
	failoverErr *service.UpstreamFailoverError,
) (action FailoverAction) {
	started := time.Now()
	reason := "retry_policy_stopped"
	defer func() {
		service.RecordSchedulingDecision(ctx, service.SchedulingDecision{Stage: "retry", Action: "decided", Reason: reason, AccountID: accountID, DurationMs: time.Since(started).Milliseconds()})
		logger.FromContext(ctx).Info("gateway.retry_decision", zap.String("reason", reason), zap.Int("action", int(action)), zap.Int64("account_id", accountID), zap.Int("switch_count", s.SwitchCount), zap.Int("same_account_retry_count", s.SameAccountRetryCount[accountID]))
	}()
	// 客户端已断开：failover 只会用已取消的 context 重新选号并必然失败，
	// 不应再被当成账号耗尽处理（误报 502）。
	if ctx != nil && ctx.Err() != nil {
		reason = "client_canceled"
		return FailoverCanceled
	}
	s.LastFailoverErr = failoverErr
	if failoverErr == nil || !service.OpenAITurnRetryAllowed(ctx, accountID, nil) || (!s.UncertainReplaySafe && !failoverErr.RequestNotSent && !failoverErr.RejectedBeforeGeneration && failoverErr.StatusCode >= 500) {
		return FailoverExhausted
	}

	if budgetErr := service.PrepareTurnRecovery(ctx, failoverErr); budgetErr != nil {
		s.LastFailoverErr = &service.UpstreamFailoverError{StatusCode: 503, Stage: "selection", Reason: "preparation_budget_exhausted", Cause: budgetErr, ClientMessage: budgetErr.Error()}
		return FailoverExhausted
	}

	// 同账号重试不算切换账号，粘性会话仅在实际切换时强制缓存计费。
	retryCount := s.SameAccountRetryCount[accountID]
	sameAccountRetry := sameAccountRetryAllowed(failoverErr, retryCount, retryLimit)
	if needForceCacheBilling(s.hasBoundSession, failoverErr, sameAccountRetry) {
		s.ForceCacheBilling = true
	}

	// 同账号重试：对 RetryableOnSameAccount 的临时性错误，先在同一账号上重试。
	// 重试次数上限 retryLimit 由调用方传入（账号级 pool_mode_retry_count 配置）。
	if sameAccountRetry {
		reason = "same_account_retry"
		s.SameAccountRetryCount[accountID]++
		retryDelay := sameAccountRetryDelayFor(failoverErr, s.SameAccountRetryCount[accountID])
		logger.FromContext(ctx).Warn("gateway.failover_same_account_retry",
			zap.Int64("account_id", accountID),
			zap.Int("upstream_status", failoverErr.StatusCode),
			zap.Int("same_account_retry_count", s.SameAccountRetryCount[accountID]),
			zap.Int("same_account_retry_max", retryLimit),
			zap.Duration("retry_delay", retryDelay),
		)
		if waitErr := service.WaitOpenAITurnRetry(ctx, retryDelay); waitErr != nil {
			reason = "retry_wait_exhausted"
			if ctx.Err() != nil {
				reason = "client_canceled"
				return FailoverCanceled
			}
			s.LastFailoverErr = &service.UpstreamFailoverError{StatusCode: 503, Stage: "selection", Cause: waitErr, Reason: service.GatewayFailureReason(reason), ClientMessage: waitErr.Error()}
			return FailoverExhausted
		}
		return FailoverContinue
	}

	service.ExcludeTurnRecoveryAccount(ctx, service.TurnRetryAccount(ctx), failoverErr)
	service.PinTurnRetryAccount(ctx, nil)
	if !failoverErr.ShouldRetryNextAccount() || !s.ReplayAllowed {
		return FailoverExhausted
	}
	// 同账号重试用尽，执行临时封禁
	if failoverErr.RetryableOnSameAccount {
		gatewayService.TempUnscheduleRetryableError(ctx, accountID, failoverErr)
	}

	// 加入失败列表
	s.FailedAccountIDs[accountID] = struct{}{}

	// 检查是否耗尽
	if s.SwitchCount >= s.MaxSwitches {
		reason = "switch_budget_exhausted"
		return FailoverExhausted
	}

	// 递增切换计数
	reason = "switch_account"
	s.SwitchCount++
	logger.FromContext(ctx).Warn("gateway.failover_switch_account",
		zap.Int64("account_id", accountID),
		zap.Int("upstream_status", failoverErr.StatusCode),
		zap.Int("switch_count", s.SwitchCount),
		zap.Int("max_switches", s.MaxSwitches),
	)

	// Antigravity 平台换号线性递增延时
	if platform == service.PlatformAntigravity {
		delay := time.Duration(s.SwitchCount-1) * time.Second
		if waitErr := service.WaitOpenAITurnRetry(ctx, delay); waitErr != nil {
			if ctx.Err() == nil {
				reason = "retry_wait_exhausted"
				s.LastFailoverErr = &service.UpstreamFailoverError{StatusCode: 503, Stage: "selection", Cause: waitErr, Reason: "retry_wait_exhausted", ClientMessage: waitErr.Error()}
				return FailoverExhausted
			}
			reason = "client_canceled"
			return FailoverCanceled
		}
	}

	return FailoverContinue
}

// HandleSelectionExhausted advances an unavailable pinned owner only after replay
// checks; it never resets exclusions because a previous attempt returned 503.
func (s *FailoverState) HandleSelectionExhausted(ctx context.Context, selectionErrors ...error) FailoverAction {
	if ctx.Err() != nil {
		return FailoverCanceled
	}
	// Dependency errors and permanent exclusions never reopen the failed pool.
	if len(selectionErrors) > 0 && !errors.Is(selectionErrors[0], service.ErrNoAvailableAccounts) {
		return FailoverExhausted
	}
	if pinned := service.TurnRetryAccount(ctx); pinned != nil && s.LastFailoverErr != nil && s.LastFailoverErr.ShouldRetryNextAccount() && s.ReplayAllowed && s.SwitchCount < s.MaxSwitches {
		service.ExcludeTurnRecoveryAccount(ctx, pinned, s.LastFailoverErr)
		service.PinTurnRetryAccount(ctx, nil)
		s.FailedAccountIDs[pinned.ID] = struct{}{}
		s.SwitchCount++
		return FailoverContinue
	}
	return FailoverExhausted
}

// needForceCacheBilling 判断 failover 时是否需要强制缓存计费。
// 粘性会话实际切换账号、或上游明确标记时，将 input_tokens 转为 cache_read 计费。
func needForceCacheBilling(hasBoundSession bool, failoverErr *service.UpstreamFailoverError, sameAccountRetry bool) bool {
	return (hasBoundSession && !sameAccountRetry) || (failoverErr != nil && failoverErr.ForceCacheBilling)
}

// failoverClientGone 判断下游客户端是否已断开（请求 context 已取消）。
// 客户端断开后 failover 必须静默终止：用已取消的 context 重新选号只会得到
// context.Canceled，并被误报成账号耗尽（通用 502）；上游 detach 的在途请求
// 照常完成计费，但不再为无人接收的响应启动新的上游尝试。
// 响应尚未提交时把状态码标记为 499（client closed request），供访问日志归类。
func failoverClientGone(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Context().Err() == nil {
		return false
	}
	// 先停 compact 心跳（接管 ResponseWriter，建立 happens-before），与
	// handleStreamingAwareError/errorResponse 等终结路径对齐，避免心跳
	// goroutine 与下面的状态标记并发触碰同一 writer。心跳已提交 200 时
	// 状态码已固化，不再标 499。
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		return true
	}
	if !c.Writer.Written() {
		c.Status(statusClientClosedRequest)
	}
	return true
}

// sleepWithContext 等待指定时长，返回 false 表示 context 已取消。
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
