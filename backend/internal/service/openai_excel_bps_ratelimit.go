package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// ExcelBPSRateLimitedReason marks a BPS 429 that another account may still
// serve. It doubles as the client-facing error code once failover is exhausted.
const ExcelBPSRateLimitedReason = GatewayFailureReason("basispoints_rate_limited")

const excelBPSRateLimitedClientMessage = "Excel BPS rate limit exceeded, please retry later"

// excelBPSRateLimitedFilterReason names BPS cooldowns in "no available
// accounts" diagnostics; the handler reports such pools as rate limited.
const excelBPSRateLimitedFilterReason = "excel_bps_rate_limited"

// Request/WS-turn state shares the existing turn budget lifecycle. It stores
// only route/account decisions, never ciphertext, credentials or request data.
type excelBPSPoolRecovery struct {
	managed, native bool
	nativeAvailable bool
	rejected        map[int64]bool
	nativeAttempts  map[int64]bool
}

type excelBPSNativeProbeKey struct{}

func excelBPSNativeRecoveryActive(ctx context.Context) bool {
	if probe, _ := ctx.Value(excelBPSNativeProbeKey{}).(bool); probe {
		return true
	}
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if budget == nil {
		return false
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.bpsRecovery.native
}

// Apply the native phase to fresh snapshots too (slot/profit rechecks may have
// replaced the selected account). Persisted BPS settings are never changed.
func excelBPSPoolForwardAccount(ctx context.Context, c *gin.Context, account *Account) *Account {
	if !excelBPSHasNativeFallback(account) || !excelBPSNativeRecoveryActive(ctx) {
		return account
	}
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	budget.mu.Lock()
	if budget.bpsRecovery.nativeAttempts == nil {
		budget.bpsRecovery.nativeAttempts = make(map[int64]bool)
	}
	first := !budget.bpsRecovery.nativeAttempts[account.ID]
	budget.bpsRecovery.nativeAttempts[account.ID] = true
	budget.mu.Unlock()
	if first {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Kind: "recovery", Scope: "excel_bps", Stage: "protocol_fallback", Reason: "bps_pool_exhausted",
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			Message: "Available BPS routes exhausted; retrying with native Codex authorization",
		})
	}
	return excelBPSNativeAccount(account)
}

func (s *OpenAIGatewayService) excelBPSPoolCandidate(ctx context.Context, account *Account, model string, excluded bool) (*Account, bool) {
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if budget == nil {
		return account, !excluded
	}
	budget.mu.Lock()
	native, active := budget.bpsRecovery.native, len(budget.bpsRecovery.rejected) > 0
	if probe, _ := ctx.Value(excelBPSNativeProbeKey{}).(bool); probe {
		native, active = true, true
	}
	budget.mu.Unlock()
	if !active {
		return account, !excluded
	}
	if native {
		if excluded || !excelBPSHasNativeFallback(account) {
			return account, false
		}
		return excelBPSNativeAccount(account), true
	}
	return account, !excluded && s.excelBPSGloballyEnabled(ctx) && account.IsExcelBPSEnabledForModel(model) && !s.isExcelBPSCoolingDown(account, model)
}

// BPS throttles its own endpoint independently of the account's Codex quota.
// A BPS 429 cools only this account's BPS route. Recoverable requests exhaust
// eligible BPS accounts before selecting native Codex grants. Persisted quota fields and the shared
// scheduler health score are left alone. Like other runtime blocks, the
// cooldown is local to this process.
func newExcelBPSRateLimitedFailoverError(retryAfter string) *UpstreamFailoverError {
	failoverErr := &UpstreamFailoverError{
		SafeToFailoverAfterWrite: true, // only keepalives can precede this rejection
		StatusCode:               http.StatusTooManyRequests,
		Stage:                    GatewayFailureStageInference,
		Scope:                    GatewayFailureScopeAccount,
		Reason:                   ExcelBPSRateLimitedReason,
		NextAccountAction:        NextAccountRetry,
		ClientStatusCode:         http.StatusTooManyRequests,
		ClientMessage:            excelBPSRateLimitedClientMessage,
	}
	// The BPS body may echo request data and is never forwarded. Only a valid
	// Retry-After survives; the handler checks it again before responding.
	if _, ok := excelBPSRetryAfter(retryAfter, time.Now()); ok {
		failoverErr.ResponseHeaders = http.Header{"Retry-After": {strings.TrimSpace(retryAfter)}}
	}
	return failoverErr
}

func excelBPSHasNativeFallback(account *Account) bool {
	return account != nil && account.IsExcelBPSEligible() && !account.IsExcelOAuth() && account.VendorOpenAIModeAllowed(VendorOpenAIModeNative) &&
		(strings.TrimSpace(account.GetOpenAIAccessToken()) != "" || strings.TrimSpace(account.GetOpenAIRefreshToken()) != "")
}

func excelBPSNativeAccount(account *Account) *Account {
	native := *account
	native.Extra = cloneExcelBPSRecoveryExtra(account.Extra)
	native.Extra["openai_excel_bps"] = false
	return &native
}

// Self-contained reasoning/compaction ciphertext is not evidence of account
// ownership. Replay it unchanged, but only before BPS starts generation; keep
// rejecting server-side references, encrypted tasks and incomplete tool history.
func excelBPSReplayableEncryptedHistory(body []byte) bool {
	if openAIResponsesAccountReplayBlocker(body) != "encrypted_content" ||
		openAIResponsesReplayBlocker(body, true) != "" || openAIWSExecutionReplayBlocker(body, true) != "" {
		return false
	}
	coverage := AnalyzeToolCallOutputContextCoverageBytes(body)
	return !coverage.HasFunctionCallOutput || coverage.ContextCoversAllCallIDs
}

// OpenAIResponsesBPSPreGenerationReplayAllowed is a narrow recovery exception
// for an explicit BPS 429 before inference/compaction. Other failures retain
// the native recovery contract. The caller still enforces output/turn budgets.
func OpenAIResponsesBPSPreGenerationReplayAllowed(body []byte, failure *UpstreamFailoverError) bool {
	return failure != nil && failure.Reason == ExcelBPSRateLimitedReason &&
		failure.StatusCode == http.StatusTooManyRequests && (failure.RequestNotSent || failure.RejectedBeforeGeneration) &&
		excelBPSReplayableEncryptedHistory(body)
}

// Never change a shared account snapshot, saved BPS switch or model/proxy.
func excelBPSNativeFallbackAccount(ctx context.Context, c *gin.Context, account *Account, body []byte, err error) *Account {
	var rejected *UpstreamFailoverError
	if !errors.As(err, &rejected) || rejected == nil || (rejected.Reason != ExcelBPSRateLimitedReason && rejected.Reason != ExcelBPSUpstreamFailureReason && rejected.Reason != "basispoints_credentials_rejected" && rejected.Reason != "basispoints_credentials_unavailable") || (rejected.Reason == ExcelBPSRateLimitedReason && rejected.StatusCode != http.StatusTooManyRequests) ||
		c.GetBool(bpsAccountProbeRequiredContextKey) || isExcelBPSClientCancellation(c, ctx.Err()) {
		return nil
	}
	if !OpenAIResponsesAccountReplayAllowed(body) || !openAIWSUncertainExecutionReplaySafe(body) {
		if !OpenAIResponsesBPSPreGenerationReplayAllowed(body, rejected) {
			return nil
		}
	}
	coverage := AnalyzeToolCallOutputContextCoverageBytes(body)
	if coverage.HasFunctionCallOutput && !coverage.ContextCoversAllCallIDs {
		return nil
	}
	if budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); budget != nil {
		budget.mu.Lock()
		managed := budget.bpsRecovery.managed
		if managed && !budget.bpsRecovery.native {
			if budget.bpsRecovery.rejected == nil {
				budget.bpsRecovery.rejected = make(map[int64]bool)
			}
			budget.bpsRecovery.rejected[account.ID] = true
		}
		budget.mu.Unlock()
		if managed {
			return nil // exhaust eligible BPS accounts before entering the native phase
		}
	}
	if !excelBPSHasNativeFallback(account) {
		return nil
	}
	SetActualOpenAIUpstreamEndpoint(c, "/v1/responses")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Kind: "recovery", Scope: "excel_bps", Stage: "protocol_fallback", Reason: string(ExcelBPSRateLimitedReason),
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
		Message: "BPS rate limited; retrying the same request with this account's native Codex authorization",
	})
	return excelBPSNativeAccount(account)
}

// A rejected BPS stream/compaction can still report usage. Preserve it exactly
// once, including when the native attempt fails before producing a result.
func finishExcelBPSNativeFallback(result **OpenAIForwardResult, previous *OpenAIForwardResult, account *Account) {
	if *result == nil {
		*result = previous
		return
	}
	if (*result).UpstreamEndpoint == "" {
		(*result).UpstreamEndpoint = "/v1/responses"
	}
	if previous != nil {
		if openAIUsageHasTokens(&previous.Usage) {
			prior := *previous
			if account.IsExcelBPSCacheCreationAsInputEnabled() {
				prior.Usage.CacheCreationInputTokens = 0
			}
			(*result).PriorAuthorizationResults = append((*result).PriorAuthorizationResults, &prior)
			usage := (*result).Usage
			(*result).authorizationUsage = &usage
		}
		usage := previous.Usage
		if account.IsExcelBPSCacheCreationAsInputEnabled() {
			usage.CacheCreationInputTokens = 0 // total input already includes BPS cache writes
		}
		addImagePolicyUsage(&(*result).Usage, usage)
	}
}

func (s *OpenAIGatewayService) isExcelBPSRateLimitBlocking(account *Account, model string) bool {
	return s.isExcelBPSCoolingDown(account, model) && !excelBPSHasNativeFallback(account)
}

// coolDownExcelBPS skips the account for BPS requests until the upstream
// Retry-After, or the configured 429 fallback when none is given. A shorter
// cooldown never replaces a longer one.
func (s *OpenAIGatewayService) coolDownExcelBPS(ctx context.Context, account *Account, retryAfter string) {
	if s == nil || account == nil {
		return
	}
	cooldown, ok := excelBPSRetryAfter(retryAfter, time.Now())
	if !ok {
		if cooldown, ok = s.excelBPS429FallbackCooldown(ctx, account); !ok {
			return
		}
	}
	// Same bounds as the configurable 429 fallback.
	if cooldown < time.Second {
		cooldown = time.Second
	}
	if limit := maxRateLimit429CooldownSeconds * time.Second; cooldown > limit {
		cooldown = limit
	}
	until := time.Now().Add(cooldown)
	for {
		current, loaded := s.excelBPSCooldownUntil.LoadOrStore(account.ID, until)
		if !loaded {
			break
		}
		if currentUntil, valid := current.(time.Time); valid && !until.After(currentUntil) {
			return
		}
		if s.excelBPSCooldownUntil.CompareAndSwap(account.ID, current, until) {
			break
		}
	}
	logger.LegacyPrintf("service.openai_excel_bps", "rate limited; skipping account for BPS requests: account_id=%d cooldown=%s", account.ID, cooldown)
}

func (s *OpenAIGatewayService) excelBPS429FallbackCooldown(ctx context.Context, account *Account) (time.Duration, bool) {
	if s.rateLimitService == nil {
		return defaultRateLimit429CooldownSeconds * time.Second, true
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	return s.rateLimitService.get429FallbackCooldown(stateCtx, account)
}

// excelBPSRetryAfter accepts delta-seconds or an HTTP date.
func excelBPSRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return 0, false
	}
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return time.Duration(seconds) * time.Second, true
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		return retryAt.Sub(now), true
	}
	return 0, false
}

// isExcelBPSCoolingDown reports a BPS cooldown only for requests this account
// would route through BPS.
func (s *OpenAIGatewayService) isExcelBPSCoolingDown(account *Account, requestedModel string) bool {
	if s == nil || account == nil {
		return false
	}
	return !s.excelBPSCooldownDeadline(account.ID).IsZero() &&
		account.IsExcelBPSEnabledForModel(requestedModel) && s.excelBPSGloballyEnabled(context.Background())
}

func (s *OpenAIGatewayService) excelBPSCooldownDeadline(accountID int64) time.Time {
	value, ok := s.excelBPSCooldownUntil.Load(accountID)
	if !ok {
		return time.Time{}
	}
	if until, valid := value.(time.Time); valid && time.Now().Before(until) {
		return until
	}
	s.excelBPSCooldownUntil.CompareAndDelete(accountID, value)
	return time.Time{}
}
