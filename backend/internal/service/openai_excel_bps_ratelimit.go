package service

import (
	"context"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
)

// ExcelBPSRateLimitedReason marks a BPS 429 that another account may still
// serve. It doubles as the client-facing error code once failover is exhausted.
const ExcelBPSRateLimitedReason = GatewayFailureReason("basispoints_rate_limited")

const excelBPSRateLimitedClientMessage = "Excel BPS rate limit exceeded, please retry later"

// excelBPSRateLimitedFilterReason names BPS cooldowns in "no available
// accounts" diagnostics; the handler reports such pools as rate limited.
const excelBPSRateLimitedFilterReason = "excel_bps_rate_limited"

var excelBPSRetryMessagePattern = regexp.MustCompile(`(?i)\btry again in\s+([0-9]+(?:\.[0-9]+)?)\s*(milliseconds?|ms|seconds?|secs?|s|minutes?|mins?|m)\b`)

func excelBPSUpstreamError(payload []byte) gjson.Result {
	if !gjson.ValidBytes(payload) {
		return gjson.Result{}
	}
	for _, path := range []string{"response.error", "error"} {
		if value := gjson.GetBytes(payload, path); value.IsObject() {
			return value
		}
	}
	if gjson.GetBytes(payload, "type").String() == "error" {
		return gjson.ParseBytes(payload)
	}
	return gjson.Result{}
}

// excelBPSRateLimitDelay reads only the error envelope, never echoed input or
// generated output: HTTP Retry-After, then the upstream's retry-after-ms /
// retry-after hints, then "try again in ...". Delays are bounded like the local
// BPS cooldown.
func excelBPSRateLimitDelay(header string, payload []byte) (time.Duration, bool) {
	now := time.Now()
	bound := func(delay time.Duration) (time.Duration, bool) {
		if delay <= 0 {
			return 0, false
		}
		return min(delay, maxRateLimit429CooldownSeconds*time.Second), true
	}
	if delay, ok := excelBPSRetryAfter(header, now); ok {
		return bound(delay)
	}
	upstreamError := excelBPSUpstreamError(payload)
	if value := upstreamError.Get("headers.retry-after-ms"); value.Exists() {
		if ms, err := strconv.ParseFloat(strings.TrimSpace(value.String()), 64); err == nil && !math.IsInf(ms, 0) && !math.IsNaN(ms) {
			return bound(time.Duration(math.Min(ms, maxRateLimit429CooldownSeconds*1000) * float64(time.Millisecond)))
		}
	}
	if value := upstreamError.Get("headers.retry-after"); value.Exists() {
		if delay, ok := excelBPSRetryAfter(value.String(), now); ok {
			return bound(delay)
		}
	}
	message := upstreamError.Get("message").String()
	if len(message) > 4096 {
		return 0, false
	}
	match := excelBPSRetryMessagePattern.FindStringSubmatch(message)
	if len(match) != 3 {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || math.IsInf(value, 0) || value <= 0 {
		return 0, false
	}
	unit := strings.ToLower(match[2])
	switch {
	case unit == "ms" || strings.HasPrefix(unit, "millisecond"):
		value /= 1000
	case strings.HasPrefix(unit, "m"):
		value *= 60
	}
	return bound(time.Duration(math.Min(value, maxRateLimit429CooldownSeconds) * float64(time.Second)))
}

// excelBPSRateLimitRetryAfter is a Retry-After value in whole seconds, rounded
// up. In particular, 173ms must not become a zero-delay loop.
func excelBPSRateLimitRetryAfter(header string, payload []byte) string {
	if _, ok := excelBPSRetryAfter(header, time.Now()); ok {
		return strings.TrimSpace(header)
	}
	delay, ok := excelBPSRateLimitDelay("", payload)
	if !ok {
		return ""
	}
	return strconv.FormatInt(excelBPSCeilSeconds(delay), 10)
}

func excelBPSCeilSeconds(delay time.Duration) int64 {
	if delay <= time.Second {
		return 1
	}
	return int64((delay + time.Second - 1) / time.Second)
}

// excelBPSClientRateLimitDelay is advertised once local retries are exhausted.
// It never drops below one second so clients do not hammer a saturated pool.
func excelBPSClientRateLimitDelay(header string, payload []byte) time.Duration {
	if delay, ok := excelBPSRateLimitDelay(header, payload); ok && delay > time.Second {
		return delay
	}
	return time.Second
}

// The upstream message can name the provider organization, so clients get a
// fixed text. Keep the standard code and "try again in" hint: OpenAI clients,
// including Codex, derive their retry delay only from rate_limit_exceeded.
const excelBPSClientRateLimitCode = "rate_limit_exceeded"

func excelBPSClientRateLimitMessage(delay time.Duration) string {
	hint := strconv.FormatFloat(delay.Round(time.Millisecond).Seconds(), 'f', -1, 64) + "s"
	return "Upstream BPS rate limit reached. Please try again in " + hint + "."
}

// excelBPSRateLimitOpsDetail keeps the upstream rejection for Ops under the
// same redaction and opt-in as other BPS error bodies.
func (s *OpenAIGatewayService) excelBPSRateLimitOpsDetail(payload []byte, token string, account *Account) (message, detail string) {
	message = "Excel BPS upstream rate limit"
	if s == nil || s.cfg == nil || !s.cfg.Gateway.LogUpstreamErrorBody {
		return message, ""
	}
	upstreamError := excelBPSUpstreamError(payload)
	if !upstreamError.IsObject() {
		return message, ""
	}
	safeBody := excelBPSSanitizeErrorBody(`{"error":`+upstreamError.Raw+`}`, token, account)
	maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
	if maxBytes <= 0 {
		maxBytes = 2048
	}
	detail, _ = sanitizeErrorBodyForStorage(safeBody, maxBytes)
	if upstreamMessage := strings.TrimSpace(extractUpstreamErrorMessage([]byte(safeBody))); upstreamMessage != "" {
		message = truncateString(upstreamMessage, 2048)
	}
	return message, detail
}

func isExcelBPSStreamRateLimit(payload []byte) bool {
	switch gjson.GetBytes(payload, "type").String() {
	case "response.failed", "response.incomplete", "error":
	default:
		return false
	}
	upstreamError := excelBPSUpstreamError(payload)
	for _, field := range []string{"code", "type"} {
		switch strings.ToLower(upstreamError.Get(field).String()) {
		case "rate_limit_exceeded", "rate_limit_error":
			return true
		}
	}
	// Some BPS streams omit the error code. Match the upstream's specific
	// RPM/TPM rejection, not a general mention of limits in an arbitrary error.
	if upstreamError.Get("code").String() != "" {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(upstreamError.Get("message").String()))
	return strings.HasPrefix(message, "rate limit reached for ") &&
		(strings.Contains(message, "tokens per min") || strings.Contains(message, "requests per min"))
}

// BPS throttles its own endpoint independently of the account's Codex quota.
// A BPS 429 is therefore handed back to the handler's account-switch loop
// before any response byte is written, and cools only this account's BPS
// route. Natively forwarded models, persisted quota fields and the shared
// scheduler health score are left alone. Like other runtime blocks, the
// cooldown is local to this process.
func newExcelBPSRateLimitedFailoverError(retryAfter string) *UpstreamFailoverError {
	failoverErr := &UpstreamFailoverError{
		StatusCode:        http.StatusTooManyRequests,
		Stage:             GatewayFailureStageInference,
		Scope:             GatewayFailureScopeAccount,
		Reason:            ExcelBPSRateLimitedReason,
		NextAccountAction: NextAccountRetry,
		ClientStatusCode:  http.StatusTooManyRequests,
		ClientMessage:     excelBPSRateLimitedClientMessage,
	}
	// The BPS body may echo request data and is never forwarded. Only a valid
	// Retry-After survives; the handler checks it again before responding.
	if _, ok := excelBPSRetryAfter(retryAfter, time.Now()); ok {
		failoverErr.ResponseHeaders = http.Header{"Retry-After": {strings.TrimSpace(retryAfter)}}
	}
	return failoverErr
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
	return s.excelBPSCooldownEndContext(context.Background(), account, requestedModel) != nil
}

func (s *OpenAIGatewayService) isExcelBPSCoolingDownContext(ctx context.Context, account *Account, requestedModel string) bool {
	return s.excelBPSCooldownEndContext(ctx, account, requestedModel) != nil
}

// Channel mapping is already reflected in the forwarded body. Cooldowns must
// use the same model as Forward, including capability rechecks that pass a
// different scheduling model. BPS selection precedes native compact mapping.
// The deadline also lets pool diagnosis return a real Retry-After without
// writing this process-local state into the account's global cooldown.
func (s *OpenAIGatewayService) excelBPSCooldownEndContext(ctx context.Context, account *Account, requestedModel string) *time.Time {
	if s == nil || account == nil {
		return nil
	}
	value, ok := s.excelBPSCooldownUntil.Load(account.ID)
	if !ok {
		return nil
	}
	if until, valid := value.(time.Time); valid && time.Now().Before(until) {
		if forward, ok := openAIForwardModelFromContext(ctx); ok {
			requestedModel = forward.model
		}
		if account.IsExcelBPSEnabledForModel(requestedModel) {
			return &until
		}
		return nil
	}
	s.excelBPSCooldownUntil.CompareAndDelete(account.ID, value)
	return nil
}
