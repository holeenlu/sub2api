package handler

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TempUnscheduler is implemented by GatewayService. Health policy stays there.
type TempUnscheduler interface {
	TempUnscheduleRetryableError(context.Context, int64, *service.UpstreamFailoverError)
}

const profitVetoExhaustedMessage = "No available accounts: all candidates rejected by group profit control"

// FailoverState retains only ingress error/scheduler payloads. Embedded Recovery
// is the sole owner of counters, exclusions, replay and cache-billing state; no
// values are copied back and forth. Remove this adapter when remaining service
// error/Account consumers use typed execution facts and scheduler ports.
type FailoverState struct {
	*execution.Recovery
	LastFailoverErr *service.UpstreamFailoverError
	OpenAIOAuth429  service.OpenAIOAuth429FailoverState
}

func NewFailoverState(maxSwitches int, hasBoundSession bool) *FailoverState {
	return &FailoverState{Recovery: execution.NewRecovery(maxSwitches, hasBoundSession)}
}
func recoveryFailure(f *service.UpstreamFailoverError) *execution.RecoveryFailure {
	if f == nil {
		return nil
	}
	return &execution.RecoveryFailure{
		StatusCode: f.StatusCode, ContentRefusal: service.IsUpstreamContentRefusal(f.ResponseBody),
		RetryNextAccount: f.ShouldRetryNextAccount(), RetryableOnSameAccount: f.RetryableOnSameAccount,
		SameAccountRetryDelay: f.SameAccountRetryDelay, SameAccountRetryDeadline: f.SameAccountRetryDeadline,
		SameAccountRetryMax: f.SameAccountRetryMax, RequestScopedTransient: f.RequestScopedTransient,
		RequestNotSent: f.RequestNotSent, RejectedBeforeGeneration: f.RejectedBeforeGeneration,
		ForceCacheBilling: f.ForceCacheBilling, CredentialFailure: f.IsCredentialFailure(),
		TargetExhausted: f.Reason == "recovery_target_exhausted", CapacityShed: f.IsOpenAICapacityShed(),
		SwitchLimitExempt: f.Reason == service.ExcelBPSRateLimitedReason,
	}
}

// Thin adapters for image/search/media consumers outside this write set. Remove
// them when those loops consume Recovery directly; none contains retry policy.
func sameAccountRetryDelayFor(f *service.UpstreamFailoverError, count int) time.Duration {
	return execution.SameAccountRetryDelay(recoveryFailure(f), count)
}
func sameAccountRetryAllowed(f *service.UpstreamFailoverError, count, limit int) bool {
	return execution.SameAccountRetryAllowed(recoveryFailure(f), count, limit, time.Now())
}
func effectiveSameAccountRetryLimit(f *service.UpstreamFailoverError, account *service.Account) int {
	if account == nil {
		return 0
	}
	return execution.EffectiveSameAccountRetryLimit(recoveryFailure(f), account.GetPoolModeRetryCount())
}

// failoverRuntime binds narrow callbacks to the existing request owner. It owns
// no budget, never sends, and does not start background work. Response-aware
// gating preserves the native route's correlated-failure accounting.
type failoverRuntime struct {
	accountID    int64
	account      *service.Account
	failure      *service.UpstreamFailoverError
	responseGate bool
	unscheduler  TempUnscheduler
	scheduler    openAIRecoveryScheduler
	state        *FailoverState
}

var _ execution.RecoveryBudget = (*failoverRuntime)(nil)
var _ execution.RecoveryAccounts = (*failoverRuntime)(nil)

func (p *failoverRuntime) OutputCommitted(ctx context.Context) bool {
	return service.TurnOutputCommitted(ctx)
}
func (p *failoverRuntime) RetryAllowed(ctx context.Context) bool {
	var body []byte
	if p.responseGate && p.failure != nil {
		body = p.failure.ResponseBody
	}
	return service.OpenAITurnRetryAllowed(ctx, p.accountID, body)
}
func (p *failoverRuntime) Prepare(ctx context.Context) error {
	return service.PrepareTurnRecovery(ctx, p.failure)
}
func (p *failoverRuntime) Wait(ctx context.Context, delay time.Duration) error {
	return service.WaitOpenAITurnRetry(ctx, delay)
}
func (p *failoverRuntime) Pin(ctx context.Context) { service.PinTurnRetryAccount(ctx, p.account) }
func (p *failoverRuntime) Exclude(ctx context.Context) {
	service.ExcludeTurnRecoveryAccount(ctx, p.account, p.failure)
	service.PinTurnRetryAccount(ctx, nil)
}
func (p *failoverRuntime) TempUnschedule(ctx context.Context) {
	p.unscheduler.TempUnscheduleRetryableError(ctx, p.accountID, p.failure)
}
func (p *failoverRuntime) StopSwitch(_ context.Context, count int) bool {
	return p.scheduler.ShouldStopOpenAIOAuth429Failover(p.account, p.failure.StatusCode, count, &p.state.OpenAIOAuth429)
}
func (p *failoverRuntime) ports(log *zap.Logger, event string) execution.RecoveryPorts {
	return execution.RecoveryPorts{Budget: p, Accounts: p, Retrying: func(count, limit int, delay time.Duration) {
		if log != nil {
			log.Warn(event, zap.Int64("account_id", p.accountID), zap.Int("upstream_status", p.failure.StatusCode),
				zap.Int("retry_limit", limit), zap.Int("retry_count", count), zap.Duration("retry_delay", delay))
		}
	}}
}
func waitOpenAISameAccountRetry(ctx context.Context, account *service.Account, failure *service.UpstreamFailoverError, counts map[int64]int, log *zap.Logger, event string) (bool, error) {
	if account == nil || failure == nil {
		return false, nil
	}
	runtime := &failoverRuntime{accountID: account.ID, account: account, failure: failure}
	return execution.RetrySameAccount(ctx, account.ID, recoveryFailure(failure), account.GetPoolModeRetryCount(), counts, runtime.ports(log, event))
}

func (s *FailoverState) HandleFailoverError(ctx context.Context, gateway TempUnscheduler, accountID int64, platform string, retryLimit int, failure *service.UpstreamFailoverError) execution.RecoveryAction {
	started := time.Now()
	if ctx.Err() == nil {
		s.LastFailoverErr = failure
	}
	runtime := &failoverRuntime{accountID: accountID, account: service.TurnRetryAccount(ctx), failure: failure, unscheduler: gateway}
	log := logger.FromContext(ctx)
	ports := runtime.ports(log, "gateway.failover_same_account_retry")
	ports.Retrying = func(count, limit int, delay time.Duration) {
		log.Warn("gateway.failover_same_account_retry", zap.Int64("account_id", accountID), zap.Int("upstream_status", failure.StatusCode),
			zap.Int("same_account_retry_count", count), zap.Int("same_account_retry_max", limit), zap.Duration("retry_delay", delay))
	}
	ports.Switching = func(count int) {
		log.Warn("gateway.failover_switch_account", zap.Int64("account_id", accountID), zap.Int("upstream_status", failure.StatusCode),
			zap.Int("switch_count", count), zap.Int("max_switches", s.MaxSwitches))
	}
	var switchDelayStep time.Duration
	if platform == service.PlatformAntigravity {
		switchDelayStep = time.Second
	}
	result := s.Recovery.RecoverFailure(ctx, accountID, recoveryFailure(failure), retryLimit, switchDelayStep, ports)
	if result.Err != nil && result.Action != execution.RecoveryCanceled {
		s.LastFailoverErr = &service.UpstreamFailoverError{StatusCode: 503, Stage: "selection", Reason: service.GatewayFailureReason(result.Reason), Cause: result.Err, ClientMessage: result.Err.Error()}
	}
	service.RecordSchedulingDecision(ctx, service.SchedulingDecision{Stage: "retry", Action: "decided", Reason: result.Reason, AccountID: accountID, DurationMs: time.Since(started).Milliseconds()})
	log.Info("gateway.retry_decision", zap.String("reason", result.Reason), zap.Int("action", int(result.Action)),
		zap.Int64("account_id", accountID), zap.Int("switch_count", s.SwitchCount), zap.Int("same_account_retry_count", s.SameAccountRetryCount[accountID]))
	return result.Action
}
func (s *FailoverState) HandleSelectionExhausted(ctx context.Context, selectionErrors ...error) execution.RecoveryAction {
	pinned := service.TurnRetryAccount(ctx)
	var id int64
	if pinned != nil {
		id = pinned.ID
	}
	runtime := &failoverRuntime{account: pinned, failure: s.LastFailoverErr}
	emptyPool := len(selectionErrors) == 0 || errors.Is(selectionErrors[0], service.ErrNoAvailableAccounts)
	return s.Recovery.SelectionExhausted(ctx, id, pinned != nil, emptyPool, recoveryFailure(s.LastFailoverErr), runtime.ports(nil, "")).Action
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

// The scheduler still owns live account-health/429 policy, not recovery counts.
type openAIRecoveryScheduler interface {
	RecordOpenAIAccountSwitch()
	ShouldStopOpenAIOAuth429Failover(*service.Account, int, int, *service.OpenAIOAuth429FailoverState) bool
}

func (s *FailoverState) nativeRecoveryPorts(account *service.Account, failure *service.UpstreamFailoverError, scheduler openAIRecoveryScheduler, log *zap.Logger) execution.RecoveryPorts {
	var accountID int64
	if account != nil {
		accountID = account.ID
	}
	runtime := &failoverRuntime{accountID: accountID, account: account, failure: failure, responseGate: true, scheduler: scheduler, state: s}
	ports := runtime.ports(log, "openai.recovery_same_account")
	ports.Switching = func(count int) {
		scheduler.RecordOpenAIAccountSwitch()
		if log != nil {
			log.Warn("openai.recovery_switch_account", zap.Int64("account_id", accountID), zap.Int("upstream_status", failure.StatusCode),
				zap.Int("switch_count", count), zap.Int("max_switches", s.MaxSwitches))
		}
	}
	return ports
}
func recordOpenAIRecoveryStop(c *gin.Context, account *service.Account, result execution.RecoveryResult) {
	switch result.Reason {
	case "turn_budget", "context_not_portable", "account_switch_limit":
		service.RecordOpenAIRetryStop(c, account, service.OpenAIRetryStopReason(result.Reason))
	}
}
func (s *FailoverState) RecoverOpenAI(ctx context.Context, c *gin.Context, account *service.Account, failure *service.UpstreamFailoverError, portable, retrySame bool, scheduler openAIRecoveryScheduler, log *zap.Logger) (execution.RecoveryAction, error) {
	// Missing service entities cannot be projected. Cancellation keeps priority;
	// all valid-input recovery policy is owned by execution.
	if ctx.Err() != nil {
		return execution.RecoveryCanceled, ctx.Err()
	}
	if account == nil || failure == nil {
		return execution.RecoveryExhausted, nil
	}
	s.LastFailoverErr = failure
	fact := recoveryFailure(failure)
	result := s.Recovery.RecoverBoundAccount(ctx, account.ID, fact, portable, retrySame, account.GetPoolModeRetryCount(), s.nativeRecoveryPorts(account, failure, scheduler, log))
	failure.RetryableOnSameAccount, failure.SameAccountRetryMax = fact.RetryableOnSameAccount, fact.SameAccountRetryMax
	recordOpenAIRecoveryStop(c, account, result)
	return result.Action, result.Err
}

// RecoverOpenAISelection projects the selection and continuation facts. The
// core alone decides whether the failed pinned owner may be moved; the caller
// retains protocol-specific mapping of the original selection/upstream errors.
func (s *FailoverState) RecoverOpenAISelection(ctx context.Context, c *gin.Context, selectionErr error, body []byte, scheduler openAIRecoveryScheduler, log *zap.Logger) (execution.RecoveryAction, bool) {
	pinned := service.TurnRetryAccount(ctx)
	selected := execution.SelectionFailure{
		HasPinned:     pinned != nil,
		PoolExhausted: errors.Is(selectionErr, service.ErrNoAvailableAccounts),
		Portable:      openAIForwardAccountReplayAllowed(c, pinned, body, s.LastFailoverErr),
	}
	if pinned != nil {
		selected.PinnedAccountID = pinned.ID
	}
	result, handled := s.Recovery.RecoverUnavailableOwner(ctx, selected, recoveryFailure(s.LastFailoverErr), s.nativeRecoveryPorts(pinned, s.LastFailoverErr, scheduler, log))
	if handled {
		recordOpenAIRecoveryStop(c, pinned, result)
	}
	return result.Action, handled
}

// BeginOperation clears service-owned error/scheduler payloads only when the
// execution owner accepts a newer client operation. Duplicate turn callbacks
// cannot erase a failure or replenish recovery counts for the current operation.
func (s *FailoverState) BeginOperation(sequence uint64) bool {
	if !s.Recovery.BeginOperation(sequence) {
		return false
	}
	s.LastFailoverErr = nil
	s.OpenAIOAuth429 = service.OpenAIOAuth429FailoverState{}
	return true
}
