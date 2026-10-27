package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

// ExcelBPSRateLimitedReason marks a BPS 429 that another account may still
// serve. It doubles as the client-facing error code once failover is exhausted.
const ExcelBPSRateLimitedReason = GatewayFailureReason("basispoints_rate_limited")

const excelBPSRateLimitedClientMessage = "Excel BPS rate limit exceeded, please retry later"

// excelBPSRateLimitedFilterReason names BPS cooldowns in "no available
// accounts" diagnostics; the handler reports such pools as rate limited.
const excelBPSRateLimitedFilterReason = "excel_bps_rate_limited"

// BPS throttles independently of native Codex quota. Recovery may select only
// another eligible BPS account; a cooldown never changes the protocol switch.
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

// OpenAIResponsesBPSPreGenerationReplayAllowed preserves self-contained history
// across rejected BPS attempts, including credential preparation between accounts.
// It does not relax native/uncertain execution or account-owned reference checks.
func OpenAIResponsesBPSPreGenerationReplayAllowed(body []byte, failure *UpstreamFailoverError) bool {
	if failure == nil || !(failure.RequestNotSent || failure.RejectedBeforeGeneration) || IsUpstreamContentRefusal(failure.ResponseBody) {
		return false
	}
	switch failure.Reason {
	case ExcelBPSRateLimitedReason:
		if failure.StatusCode != http.StatusTooManyRequests {
			return false
		}
	case "basispoints_credentials_unavailable":
		if !failure.IsCredentialFailure() || !failure.RequestNotSent {
			return false
		}
	case "basispoints_credentials_rejected":
		if !failure.IsCredentialFailure() || (failure.StatusCode != http.StatusUnauthorized && failure.StatusCode != http.StatusForbidden) {
			return false
		}
	case ExcelBPSUpstreamFailureReason:
		if failure.StatusCode != http.StatusForbidden {
			return false
		}
	default:
		return false
	}
	return excelBPSReplayableEncryptedHistory(body)
}

func (s *OpenAIGatewayService) isExcelBPSRateLimitBlocking(account *Account, model string) bool {
	return s.isExcelBPSCoolingDown(account, model)
}

// ExcelBPSCooldownScopePrefix isolates persisted BPS state from native model
// limits in the existing account model_rate_limits store.
const ExcelBPSCooldownScopePrefix = "openai:excel_bps:"

const excelBPSAccountCooldownScope = ExcelBPSCooldownScopePrefix + "429"

// AccountExcelBPSCooldownRepository extends the existing account state store.
// Extend must atomically keep the later deadline and publish the changed account
// through the existing outbox. Get must read authoritative state, including
// extensions from another instance; absent scopes return a zero time, not an error.
type AccountExcelBPSCooldownRepository interface {
	ExtendExcelBPSCooldown(context.Context, int64, string, time.Time) (time.Time, error)
	GetExcelBPSCooldown(context.Context, int64, []string) (time.Time, error)
}

// excelBPSRateLimitScope describes confirmed upstream scope, never a client
// alias or an inferred pool-wide quota. Zero means the known account/BPS route;
// both fields set means their intersection, not two independent cooldowns.
type excelBPSRateLimitScope struct {
	model    string
	endpoint string
}

func excelBPSCooldownEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, "bps.openai.com") && parsed.User == nil {
		// The same trusted BPS endpoint is supplied as a path by failure
		// classifiers and as an absolute URL by the shared transport.
		return parsed.EscapedPath()
	}
	return endpoint
}

func (scope excelBPSRateLimitScope) key() string {
	key := excelBPSAccountCooldownScope
	if model := strings.TrimSpace(scope.model); model != "" {
		key += ":model:" + base64.RawURLEncoding.EncodeToString([]byte(model))
	}
	if endpoint := excelBPSCooldownEndpoint(scope.endpoint); endpoint != "" {
		key += ":endpoint:" + base64.RawURLEncoding.EncodeToString([]byte(endpoint))
	}
	return key
}

type excelBPSRateLimitFailure struct {
	scope      excelBPSRateLimitScope
	retryAfter string
	resetAt    time.Time
}

// parseExcelBPSRateLimitFailure consumes an actual BPS HTTP rejection or a raw
// upstream terminal event, not client JSON or a local/authorization failure.
// Only explicit error.scope ("account", "bps", "model", "endpoint", or
// "model_endpoint", also as an object with type/model/endpoint) narrows scope.
// Optional target fields must match the already-selected model/endpoint.
// Unknown scope keeps the known account/BPS default; a contradictory explicit
// target returns false rather than widening that target to the whole account.
// Body reset fields require recognized scope evidence. Native quota headers,
// model names in prose/param, and generic error codes never establish scope.
func parseExcelBPSRateLimitFailure(status int, headers http.Header, raw []byte, upstreamModel, endpoint string, now time.Time) (excelBPSRateLimitFailure, bool) {
	var result excelBPSRateLimitFailure
	if status != http.StatusTooManyRequests {
		if status != http.StatusOK && status != 0 {
			return result, false
		}
		failure := basispoints.ParseUpstreamFailure(raw)
		if failure == nil || failure.Status != http.StatusTooManyRequests {
			return result, false
		}
	}
	retryAt, validRetry := ExcelBPSRetryAfterDeadline(headers.Get("Retry-After"), now)
	if validRetry {
		result.retryAfter = strings.TrimSpace(headers.Get("Retry-After"))
	}
	var envelope struct {
		Error    json.RawMessage `json:"error"`
		Response struct {
			Error json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if len(raw) == 0 || len(raw) > 512<<10 || json.Unmarshal(raw, &envelope) != nil {
		return result, true
	}
	detailRaw := envelope.Error
	if len(envelope.Response.Error) > 0 {
		detailRaw = envelope.Response.Error
	}
	var detail struct {
		Scope           json.RawMessage `json:"scope"`
		Model           string          `json:"model"`
		Endpoint        string          `json:"endpoint"`
		RetryAfter      json.RawMessage `json:"retry_after"`
		ResetsAt        json.RawMessage `json:"resets_at"`
		ResetsInSeconds json.RawMessage `json:"resets_in_seconds"`
	}
	if json.Unmarshal(detailRaw, &detail) != nil {
		return result, true
	}
	var declared struct {
		Type     string `json:"type"`
		Model    string `json:"model"`
		Endpoint string `json:"endpoint"`
	}
	if json.Unmarshal(detail.Scope, &declared.Type) == nil {
		declared.Model, declared.Endpoint = detail.Model, detail.Endpoint
	} else if json.Unmarshal(detail.Scope, &declared) != nil {
		return result, true
	}
	upstreamModel = strings.TrimSpace(upstreamModel)
	endpoint = excelBPSCooldownEndpoint(endpoint)
	switch declared.Type {
	case "account", "bps":
	case "model", "endpoint", "model_endpoint":
		if declared.Type != "endpoint" {
			if upstreamModel == "" || (declared.Model != "" && strings.TrimSpace(declared.Model) != upstreamModel) {
				return result, false
			}
			result.scope.model = upstreamModel
		}
		if declared.Type != "model" {
			if endpoint == "" || (declared.Endpoint != "" && excelBPSCooldownEndpoint(declared.Endpoint) != endpoint) {
				return result, false
			}
			result.scope.endpoint = endpoint
		}
	default:
		return result, true
	}
	if value := excelBPSRateLimitScalar(detail.RetryAfter); value != "" {
		if until, ok := ExcelBPSRetryAfterDeadline(value, now); ok && until.After(retryAt) {
			result.retryAfter = value
		}
	}
	if value := excelBPSRateLimitScalar(detail.ResetsInSeconds); value != "" {
		if until, ok := ExcelBPSRetryAfterDeadline(value, now); ok {
			result.resetAt = until
		}
	}
	if value := excelBPSRateLimitScalar(detail.ResetsAt); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
			until := time.Unix(seconds, 0)
			if until.Year() <= 9999 && until.After(now) && until.After(result.resetAt) {
				result.resetAt = until
			}
		}
	}
	return result, true
}

func excelBPSRateLimitScalar(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func (scope excelBPSRateLimitScope) matchingKeys() []string {
	keys := []string{excelBPSAccountCooldownScope}
	if strings.TrimSpace(scope.model) != "" {
		keys = append(keys, (excelBPSRateLimitScope{model: scope.model}).key())
	}
	if strings.TrimSpace(scope.endpoint) != "" {
		keys = append(keys, (excelBPSRateLimitScope{endpoint: scope.endpoint}).key())
		if strings.TrimSpace(scope.model) != "" {
			keys = append(keys, scope.key())
		}
	}
	return keys
}

type excelBPSScopedCooldownKey struct {
	accountID int64
	scope     string
}

func excelBPSLocalCooldownKey(accountID int64, scope string) any {
	if scope == excelBPSAccountCooldownScope {
		return accountID
	}
	return excelBPSScopedCooldownKey{accountID, scope}
}

// coolDownExcelBPS is the account/BPS default for callers without narrower
// evidence. Callers with a confirmed model/endpoint/reset must use the scoped
// variant; neither path writes native quota or disables BPS.
func (s *OpenAIGatewayService) coolDownExcelBPS(ctx context.Context, account *Account, retryAfter string) {
	if _, err := s.coolDownExcelBPSScope(ctx, account, excelBPSRateLimitScope{}, retryAfter, time.Time{}); err != nil {
		logger.LegacyPrintf("service.openai_excel_bps", "persist rate limit failed: account_id=%d err=%v", account.ID, err)
	}
}

// coolDownExcelBPSScope records trusted BPS timing. resetAt must come from a
// verified BPS reset fact, never native quota headers or an unclassified body.
// An error means durability was not confirmed; the local deadline still guards
// this process and must not be reported as a successful persistent write.
func (s *OpenAIGatewayService) coolDownExcelBPSScope(ctx context.Context, account *Account, scope excelBPSRateLimitScope, retryAfter string, resetAt time.Time) (time.Time, error) {
	if s == nil || account == nil {
		return time.Time{}, nil
	}
	now := time.Now()
	until, valid := ExcelBPSRetryAfterDeadline(retryAfter, now)
	if resetAt.After(now) && resetAt.After(until) {
		until, valid = resetAt, true
	}
	if !valid {
		cooldown, enabled := s.excelBPS429FallbackCooldown(ctx, account)
		if !enabled {
			return s.excelBPSRateLimitDeadline(ctx, account.ID, scope)
		}
		until = now.Add(cooldown)
	}
	// Only fallback settings are capped. A valid upstream not-before can be
	// days away; it must survive serialization without duration truncation.
	if minimum := now.Add(time.Second); until.Before(minimum) {
		until = minimum
	}
	key := scope.key()
	until = s.extendExcelBPSLocalCooldown(account.ID, key, until)
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	if repo, ok := s.accountRepo.(AccountExcelBPSCooldownRepository); ok {
		saved, err := repo.ExtendExcelBPSCooldown(stateCtx, account.ID, key, until)
		if err != nil {
			return until, fmt.Errorf("persist BPS cooldown: %w", err)
		}
		until = s.extendExcelBPSLocalCooldown(account.ID, key, saved)
	}
	return until, nil
}

func (s *OpenAIGatewayService) extendExcelBPSLocalCooldown(accountID int64, scope string, until time.Time) time.Time {
	key := excelBPSLocalCooldownKey(accountID, scope)
	for {
		current, loaded := s.excelBPSCooldownUntil.LoadOrStore(key, until)
		if !loaded {
			return until
		}
		if currentUntil, valid := current.(time.Time); valid && !until.After(currentUntil) {
			return currentUntil
		}
		if s.excelBPSCooldownUntil.CompareAndSwap(key, current, until) {
			return until
		}
	}
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
	until, ok := ExcelBPSRetryAfterDeadline(value, now)
	if !ok {
		return 0, false
	}
	return until.Sub(now), true
}

// ExcelBPSRetryAfterDeadline validates the upstream not-before without truncating
// a valid long cooldown; callers must keep it separate from local wait budgets.
func ExcelBPSRetryAfterDeadline(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return time.Time{}, false
	}
	// Keep an absolute time: multiplying large delta-seconds by time.Second
	// can overflow a duration even when the RFC3339 deadline is representable.
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		maxSeconds := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC).Unix() - now.Unix()
		if maxSeconds >= 0 && seconds <= uint64(maxSeconds) {
			return time.Unix(now.Unix()+int64(seconds), int64(now.Nanosecond())), true
		}
		return time.Time{}, false
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt, true
	}
	return time.Time{}, false
}

// Candidate filtering only consults the account and already-loaded policy.
// A missing policy cache cannot trigger requestless I/O; the context-aware
// routing/sending owner remains responsible for refreshing the global switch.
func (s *OpenAIGatewayService) excelBPSCandidateCooldownEnabled(account *Account, requestedModel string) bool {
	if s == nil || account == nil || !account.IsExcelBPSEnabledForModel(requestedModel) {
		return false
	}
	if s.settingService != nil {
		if cached, ok := s.settingService.excelBPSEnabledCache.Load().(*cachedExcelBPSEnabled); ok && cached != nil {
			return cached.enabled
		}
	}
	return true
}

// isExcelBPSCoolingDown is an I/O-free candidate hint. Cross-instance updates
// arrive in the account snapshot; send-time authority uses the typed reader.
func (s *OpenAIGatewayService) isExcelBPSCoolingDown(account *Account, requestedModel string) bool {
	if !s.excelBPSCandidateCooldownEnabled(account, requestedModel) {
		return false
	}
	scope := excelBPSRateLimitScope{model: account.GetMappedModel(requestedModel)}
	for _, key := range scope.matchingKeys() {
		if until := account.modelRateLimitResetAt(key); until != nil && time.Now().Before(*until) {
			return true
		}
		if !s.excelBPSLocalCooldownDeadline(account.ID, key).IsZero() {
			return true
		}
	}
	return false
}

// excelBPSCooldownDeadline is the local account/BPS deadline only. It neither
// reads shared state nor manufactures a deadline when storage is unavailable.
func (s *OpenAIGatewayService) excelBPSCooldownDeadline(accountID int64) time.Time {
	return s.excelBPSLocalCooldownDeadline(accountID, excelBPSAccountCooldownScope)
}

// excelBPSRateLimitDeadline must be rechecked immediately before sending,
// including after an upload wait and for repair/probe. It never clears scopes
// or treats a missing/failed read as proof of recovery.
func (s *OpenAIGatewayService) excelBPSRateLimitDeadline(ctx context.Context, accountID int64, scope excelBPSRateLimitScope) (time.Time, error) {
	return s.excelBPSStoredCooldownDeadline(ctx, accountID, scope.matchingKeys())
}

func (s *OpenAIGatewayService) excelBPSStoredCooldownDeadline(ctx context.Context, accountID int64, scopes []string) (time.Time, error) {
	if s == nil {
		return time.Time{}, nil
	}
	var until time.Time
	for _, scope := range scopes {
		if local := s.excelBPSLocalCooldownDeadline(accountID, scope); local.After(until) {
			until = local
		}
	}
	if repo, ok := s.accountRepo.(AccountExcelBPSCooldownRepository); ok {
		if ctx == nil {
			ctx = context.Background()
		}
		readCtx, cancel := context.WithTimeout(ctx, openAIAccountStateUpdateTimeout)
		defer cancel()
		stored, err := repo.GetExcelBPSCooldown(readCtx, accountID, scopes)
		if err != nil {
			return until, fmt.Errorf("read BPS cooldown: %w", err)
		}
		if stored.After(until) {
			until = stored
		}
	}
	if !time.Now().Before(until) {
		return time.Time{}, nil
	}
	return until, nil
}

func (s *OpenAIGatewayService) excelBPSLocalCooldownDeadline(accountID int64, scope string) time.Time {
	if s == nil {
		return time.Time{}
	}
	key := excelBPSLocalCooldownKey(accountID, scope)
	value, ok := s.excelBPSCooldownUntil.Load(key)
	if !ok {
		return time.Time{}
	}
	if until, valid := value.(time.Time); valid && time.Now().Before(until) {
		return until
	}
	s.excelBPSCooldownUntil.CompareAndDelete(key, value)
	return time.Time{}
}
