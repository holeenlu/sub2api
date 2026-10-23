package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	openAIWSBetaV1Value = "responses_websockets=2026-02-04"
	openAIWSBetaV2Value = "responses_websockets=2026-02-06"

	openAIWSFallbackReasonInvalidEncryptedContent = "invalid_encrypted_content"

	openAIWSTurnStateHeader    = "x-codex-turn-state"
	openAIWSTurnMetadataHeader = "x-codex-turn-metadata"

	openAIWSLogValueMaxLen      = 160
	openAIWSHeaderValueMaxLen   = 120
	openAIWSIDValueMaxLen       = 64
	openAIWSEventLogHeadLimit   = 20
	openAIWSEventLogEveryN      = 50
	openAIWSBufferLogHeadLimit  = 8
	openAIWSBufferLogEveryN     = 20
	openAIWSPrewarmEventLogHead = 10
	openAIWSPayloadKeySizeTopN  = 6

	openAIWSPayloadSizeEstimateDepth    = 3
	openAIWSPayloadSizeEstimateMaxBytes = 64 * 1024
	openAIWSPayloadSizeEstimateMaxItems = 16

	openAIWSEventFlushBatchSizeDefault    = 4
	openAIWSEventFlushIntervalDefault     = 25 * time.Millisecond
	openAIWSPayloadLogSampleDefault       = 0.2
	openAIWSPassthroughIdleTimeoutDefault = time.Hour

	openAIWSStoreDisabledConnModeStrict   = "strict"
	openAIWSStoreDisabledConnModeAdaptive = "adaptive"
	openAIWSStoreDisabledConnModeOff      = "off"

	openAIWSIngressStagePreviousResponseNotFound = "previous_response_not_found"
	openAIWSMaxPrevResponseIDDeletePasses        = 8
)

var openAIWSLogValueReplacer = strings.NewReplacer(
	"error", "err",
	"fallback", "fb",
	"warning", "warnx",
	"failed", "fail",
)

var openAIWSIngressPreflightPingIdle = 20 * time.Second

// ErrOpenAIWSReplayUnavailable requires client context recovery, not an account
// health penalty: the gateway cannot prove a complete replay is available.
var ErrOpenAIWSReplayUnavailable = errors.New("websocket continuation requires full input context")

// ErrOpenAIWSFirstOutputBudgetExhausted is a gateway deadline reached before
// this account received the request; it must not affect account health.
var ErrOpenAIWSFirstOutputBudgetExhausted = errors.New("websocket first-output budget exhausted before upstream write")

// OpenAIWSAccountChangedError requests a fresh upstream attempt before this
// turn is sent. A database account ID alone does not prove credential lineage.
// A request-level failure has already been sent; the client may issue another
// turn on the same socket. Its failed attempt never becomes replay history.
type openAIWSTurnRejectedError struct{ cause error }

func (e *openAIWSTurnRejectedError) Error() string { return e.cause.Error() }
func (e *openAIWSTurnRejectedError) Unwrap() error { return e.cause }

type OpenAIWSAccountChangedError struct {
	PreserveContext bool
}

func (*OpenAIWSAccountChangedError) Error() string {
	return "websocket upstream account configuration changed"
}

// RefreshOpenAIWSAccount reuses the scheduler snapshot and its bounded DB
// fallback. It never changes the shared selection object or logs credentials.
func (s *OpenAIGatewayService) RefreshOpenAIWSAccount(ctx context.Context, c *gin.Context, selected *Account, groupID *int64, requestModels ...string) (*Account, error) {
	if s == nil || selected == nil || selected.Platform != PlatformOpenAI || (s.schedulerSnapshot == nil && s.accountRepo == nil) {
		return selected, nil
	}
	waitCtx, cancel := OpenAITurnWaitContext(ctx)
	defer cancel()
	var latest *Account
	var err error
	if s.schedulerSnapshot != nil {
		latest, err = s.schedulerSnapshot.GetAccount(waitCtx, selected.ID)
	} else {
		latest, err = s.accountRepo.GetByID(waitCtx, selected.ID)
	}
	if errors.Is(context.Cause(waitCtx), ErrOpenAIWSFirstOutputBudgetExhausted) {
		return nil, ErrOpenAIWSFirstOutputBudgetExhausted
	}
	if err != nil {
		return nil, NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "upstream account state unavailable", errors.Join(ErrOpenAIWSLocalClientPolicy, err))
	}
	if latest != nil && latest.UpdatedAt.Before(selected.UpdatedAt) {
		latest = selected
	}
	if latest == nil || latest.ID != selected.ID || latest.Platform != selected.Platform || !latest.IsActive() || !latest.Schedulable || !openAIStickyAccountMatchesGroup(latest, groupID) {
		return nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "upstream account authorization is no longer valid", ErrOpenAIWSLocalClientPolicy)
	}
	oldSource, newSource := selected, latest
	if latest.IsShadow() {
		oldSource = codexAccountIdentitySource(c, selected)
		newSource = (&defaultOpenAIAccountScheduler{service: s}).lookupShadowParentAccount(waitCtx, *latest.ParentAccountID)
		if !parentHealthyForShadow(latest, func(int64) *Account { return newSource }) {
			return nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "upstream parent credentials are no longer valid", ErrOpenAIWSLocalClientPolicy)
		}
	}
	if latest.AutoPauseOnExpired && latest.ExpiresAt != nil && !time.Now().Before(*latest.ExpiresAt) {
		return nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "upstream account authorization expired", ErrOpenAIWSLocalClientPolicy)
	}
	defaultMode := OpenAIWSIngressModeCtxPool
	selectedBPS, latestBPS := false, false
	if len(requestModels) > 0 && s.excelBPSGloballyEnabled(ctx) {
		selectedBPS = selected.IsExcelBPSEnabledForModel(requestModels[0])
		latestBPS = latest.IsExcelBPSEnabledForModel(requestModels[0])
		if selectedBPS != latestBPS {
			return nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "BPS routing changed; reconnect with full input context", ErrOpenAIWSModelSwitchRequiresReconnect)
		}
	}
	if s.cfg != nil {
		defaultMode = s.cfg.Gateway.OpenAIWS.IngressModeDefault
		if !latestBPS && s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled && latest.ResolveOpenAIResponsesWebSocketV2Mode(defaultMode) == OpenAIWSIngressModeOff {
			return nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "websocket mode is disabled for this account", ErrOpenAIWSLocalClientPolicy)
		}
	}
	proxyURL := func(account *Account) string {
		if account.Proxy != nil {
			return account.Proxy.URL()
		}
		return ""
	}
	changed := selected.Type != latest.Type || !reflect.DeepEqual(selected.Credentials, latest.Credentials) || !reflect.DeepEqual(oldSource.Credentials, newSource.Credentials) || !reflect.DeepEqual(selected.ParentAccountID, latest.ParentAccountID) ||
		!reflect.DeepEqual(selected.ProxyID, latest.ProxyID) || proxyURL(selected) != proxyURL(latest) ||
		selected.ResolveOpenAIResponsesWebSocketV2Mode(defaultMode) != latest.ResolveOpenAIResponsesWebSocketV2Mode(defaultMode)
	changed = changed || selected.IsOpenAIPassthroughEnabled() != latest.IsOpenAIPassthroughEnabled() ||
		selected.IsOpenAIWSForceHTTPEnabled() != latest.IsOpenAIWSForceHTTPEnabled() ||
		selected.IsOpenAIResponsesFlattenNamespacesEnabled() != latest.IsOpenAIResponsesFlattenNamespacesEnabled() ||
		selected.IsOpenAIWSAllowStoreRecoveryEnabled() != latest.IsOpenAIWSAllowStoreRecoveryEnabled() ||
		selected.CodexImageGenerationExplicitToolPolicy() != latest.CodexImageGenerationExplicitToolPolicy() ||
		!reflect.DeepEqual(selected.CodexImageGenerationBridgeOverride(), latest.CodexImageGenerationBridgeOverride()) ||
		selected.IsCodexCLIOnlyEnabled() != latest.IsCodexCLIOnlyEnabled() || selected.IsCodexCLIOnlyAppServerAllowed() != latest.IsCodexCLIOnlyAppServerAllowed()
	if selectedBPS {
		// BPS options participate in the existing account snapshot boundary.
		// Otherwise the ingress owner would keep its old pointer for later turns.
		for key, value := range selected.Extra {
			if IsExcelBPSExtraKey(key) && !reflect.DeepEqual(value, latest.Extra[key]) {
				changed = true
			}
		}
		for key, value := range latest.Extra {
			if IsExcelBPSExtraKey(key) && !reflect.DeepEqual(value, selected.Extra[key]) {
				changed = true
			}
		}
	}
	if !changed {
		return latest, nil
	}
	preserve := oldSource.Type == newSource.Type && oldSource.ID == newSource.ID && reflect.DeepEqual(selected.ParentAccountID, latest.ParentAccountID) && selected.GetOpenAIBaseURL() == latest.GetOpenAIBaseURL() && selected.GetCredential("base_url") == latest.GetCredential("base_url") && oldSource.GetCredential("organization_id") == newSource.GetCredential("organization_id") && oldSource.GetCredential("project_id") == newSource.GetCredential("project_id")

	if newSource.IsOpenAIOAuthLike() {
		oldID, newID := oldSource.GetChatGPTAccountID(), newSource.GetChatGPTAccountID()
		preserve = preserve && oldID == newID && oldSource.GetCredential("chatgpt_user_id") == newSource.GetCredential("chatgpt_user_id") && (oldID != "" || oldSource.GetOpenAIAccessToken() == newSource.GetOpenAIAccessToken())
	} else if newSource.IsOpenAIApiKey() {
		preserve = preserve && oldSource.GetOpenAIProtocolAPIKey() == newSource.GetOpenAIProtocolAPIKey()
	} else {
		preserve = false
	}
	return latest, &OpenAIWSAccountChangedError{PreserveContext: preserve}
}

func (s *OpenAIGatewayService) openAIWSAccountRefreshRetry(account *Account, cause error, payload []byte, fullInput []json.RawMessage, complete bool, model string) error {
	var changed *OpenAIWSAccountChangedError
	if !errors.As(cause, &changed) {
		return cause
	}

	if changed.PreserveContext && !s.isOpenAIWSStoreDisabledInRequestRaw(payload, account) && gjson.GetBytes(payload, "previous_response_id").String() != "" {
		return newOpenAIWSCurrentTurnFailoverError(cause, payload)
	}
	replay, safe, err := buildOpenAIWSCurrentTurnRetryPayload(payload, fullInput, complete, model, changed.PreserveContext)
	if err != nil || !safe {
		return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "account configuration changed; reconnect with complete portable context", errors.Join(ErrOpenAIWSReplayUnavailable, err))
	}
	return newOpenAIWSCurrentTurnFailoverError(cause, replay)
}

// Use configured idle limits, not a speculative short grace period: providers
// may follow a bare error with delayed usage or even a successful response.
func (s *OpenAIGatewayService) openAIWSErrorIdleTimeout(payload []byte) time.Duration {
	timeout := s.openAIWSReadTimeout()
	if s.cfg != nil {
		seconds := s.cfg.Gateway.StreamDataIntervalTimeout
		for _, tool := range gjson.GetBytes(payload, "tools").Array() {
			if tool.Get("type").String() == "image_generation" {
				seconds = s.cfg.Gateway.ImageStreamDataIntervalTimeout
				break
			}
		}
		if seconds > 0 {
			timeout = min(timeout, time.Duration(seconds)*time.Second)
		}
	}
	return timeout
}

// The sender and recovery policy share this failure identity. A failed write
// is ambiguous, not proof that generation never started.
const (
	openAIWSFailureWriteRequest         = "write_request"
	openAIWSPayloadPrefersHTTP          = "payload_size_preferred_http"
	openAIWSOptionalPreparationTimedOut = "optional_ws_prepare_timeout"
)

// openAIWSFallbackError carries transport facts for the existing recovery owner.
// No downstream output alone does not prove that an upstream request was unsent.
type openAIWSFallbackError struct {
	Reason string
	Err    error
}

func (e *openAIWSFallbackError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("openai ws fallback: %s", strings.TrimSpace(e.Reason))
	}
	return fmt.Sprintf("openai ws fallback: %s: %v", strings.TrimSpace(e.Reason), e.Err)
}

func (e *openAIWSFallbackError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func wrapOpenAIWSFallback(reason string, err error) error {
	return &openAIWSFallbackError{Reason: strings.TrimSpace(reason), Err: err}
}

// OpenAIWSClientCloseError 表示应以指定 WebSocket close code 主动关闭客户端连接的错误。
type OpenAIWSClientCloseError struct {
	statusCode coderws.StatusCode
	reason     string
	err        error
}

type openAIWSIngressTurnError struct {
	stage           string
	cause           error
	wroteDownstream bool
	retryDetail     string // bounded progress evidence; no request/output content
}

type openAIWSCurrentTurnFailoverError struct {
	cause        error
	retryPayload []byte
}

func (e *openAIWSCurrentTurnFailoverError) Error() string {
	if e == nil || e.cause == nil {
		return "openai websocket current-turn failover"
	}
	return e.cause.Error()
}

func (e *openAIWSCurrentTurnFailoverError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newOpenAIWSCurrentTurnFailoverError(cause error, retryPayload []byte) error {
	return &openAIWSCurrentTurnFailoverError{
		cause:        cause,
		retryPayload: append([]byte(nil), retryPayload...),
	}
}

// OpenAIWSCurrentTurnRetryPayload returns an isolated copy of the payload that
// may be retried on the same account without replaying the first turn. Moving
// it to a different account requires OpenAIResponsesAccountReplayAllowed.
func OpenAIWSCurrentTurnRetryPayload(err error) ([]byte, bool) {
	var retryErr *openAIWSCurrentTurnFailoverError
	if !errors.As(err, &retryErr) || retryErr == nil {
		return nil, false
	}
	return append([]byte(nil), retryErr.retryPayload...), true
}

func (e *openAIWSIngressTurnError) Error() string {
	if e == nil {
		return ""
	}
	if e.cause == nil {
		return strings.TrimSpace(e.stage)
	}
	return e.cause.Error()
}

func (e *openAIWSIngressTurnError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func wrapOpenAIWSIngressTurnError(stage string, cause error, wroteDownstream bool) error {
	if cause == nil {
		return nil
	}
	return &openAIWSIngressTurnError{
		stage:           strings.TrimSpace(stage),
		cause:           cause,
		wroteDownstream: wroteDownstream,
	}
}

func isOpenAIWSIngressTurnRetryable(err error) bool {
	var turnErr *openAIWSIngressTurnError
	if !errors.As(err, &turnErr) || turnErr == nil {
		return false
	}
	if errors.Is(turnErr.cause, context.Canceled) || errors.Is(turnErr.cause, context.DeadlineExceeded) {
		return false
	}
	if turnErr.wroteDownstream {
		return false
	}
	switch turnErr.stage {
	case "write_upstream", "read_upstream":
		return true
	default:
		return false
	}
}

func openAIWSIngressTurnRetryReason(err error) string {
	var turnErr *openAIWSIngressTurnError
	if !errors.As(err, &turnErr) || turnErr == nil {
		return "unknown"
	}
	if turnErr.stage == "" {
		return "unknown"
	}
	return turnErr.stage
}

// IsOpenAIWSUpstreamTransportError distinguishes upstream I/O failures from
// invalid client frames and normal disconnects at the handler's Ops boundary.
func IsOpenAIWSUpstreamTransportError(err error) bool {
	var turnErr *openAIWSIngressTurnError
	return errors.As(err, &turnErr) && turnErr != nil &&
		(turnErr.stage == "read_upstream" || turnErr.stage == "write_upstream") &&
		!errors.Is(err, context.Canceled)
}

func isOpenAIWSIngressPreviousResponseNotFound(err error) bool {
	var turnErr *openAIWSIngressTurnError
	if !errors.As(err, &turnErr) || turnErr == nil {
		return false
	}
	if strings.TrimSpace(turnErr.stage) != openAIWSIngressStagePreviousResponseNotFound {
		return false
	}
	return !turnErr.wroteDownstream
}

// NewOpenAIWSClientCloseError 创建一个客户端 WS 关闭错误。
func NewOpenAIWSClientCloseError(statusCode coderws.StatusCode, reason string, err error) error {
	return &OpenAIWSClientCloseError{
		statusCode: statusCode,
		reason:     strings.TrimSpace(reason),
		err:        err,
	}
}

func (e *OpenAIWSClientCloseError) Error() string {
	if e == nil {
		return ""
	}
	if e.err == nil {
		return fmt.Sprintf("openai ws client close: %d %s", int(e.statusCode), strings.TrimSpace(e.reason))
	}
	return fmt.Sprintf("openai ws client close: %d %s: %v", int(e.statusCode), strings.TrimSpace(e.reason), e.err)
}

func (e *OpenAIWSClientCloseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *OpenAIWSClientCloseError) StatusCode() coderws.StatusCode {
	if e == nil {
		return coderws.StatusInternalError
	}
	return e.statusCode
}

func (e *OpenAIWSClientCloseError) Reason() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.reason)
}

// OpenAIWSIngressHooks 定义入站 WS 每个 turn 的生命周期回调。
type OpenAIWSIngressHooks struct {
	// ClientLifecycleContext is the request context before an ingress lease
	// adds its independent cancellation signal. Downstream writes bind to it
	// so shutdown and disconnect cancellation remain direct during lease loss.
	ClientLifecycleContext context.Context
	// InitialRequestModel is the client-facing model from the first frame,
	// before channel or account mapping. Ingress modes preserve it for usage
	// attribution while MapRequestModel determines the upstream model.
	InitialRequestModel string
	// InitialTurnStartedAt freezes when the first response.create was accepted.
	InitialTurnStartedAt time.Time
	// MaxReasoningEffort limits explicit reasoning effort values for this WS session.
	MaxReasoningEffort string
	// MaxReasoningEffortOverLimit is the access control when an explicit effort
	// exceeds the ceiling: downgrade (default) or deny.
	MaxReasoningEffortOverLimit string
	// ReasoningEffortMappings rewrites explicit effort values for this WS session.
	ReasoningEffortMappings []ReasoningEffortMapping
	TurnStarted             func(turn int, startedAt time.Time)
	BeforeTurn              func(turn int) error
	BeforeRequest           func(turn int, payload []byte, originalModel string) error
	// MapRequestModel resolves the current turn's client model to the model
	// that must be written into the upstream response.create frame.
	MapRequestModel func(turn int, originalModel string) (string, error)
	AfterTurn       func(turn int, result *OpenAIForwardResult, turnErr error)
}

func (s *OpenAIGatewayService) getOpenAIWSConnPool() *openAIWSConnPool {
	if s == nil {
		return nil
	}
	s.openaiWSPoolOnce.Do(func() {
		if s.openaiWSPool == nil {
			s.openaiWSPool = newOpenAIWSConnPool(s.cfg)
		}
	})
	return s.openaiWSPool
}

func (s *OpenAIGatewayService) getOpenAIWSPassthroughDialer() openAIWSClientDialer {
	if s == nil {
		return nil
	}
	s.openaiWSPassthroughDialerOnce.Do(func() {
		if s.openaiWSPassthroughDialer == nil {
			s.openaiWSPassthroughDialer = newDefaultOpenAIWSClientDialer()
		}
	})
	return s.openaiWSPassthroughDialer
}

func (s *OpenAIGatewayService) SnapshotOpenAIWSPoolMetrics() OpenAIWSPoolMetricsSnapshot {
	pool := s.getOpenAIWSConnPool()
	if pool == nil {
		return OpenAIWSPoolMetricsSnapshot{}
	}
	return pool.SnapshotMetrics()
}

type OpenAIWSPerformanceMetricsSnapshot struct {
	Pool      OpenAIWSPoolMetricsSnapshot      `json:"pool"`
	Retry     OpenAIWSRetryMetricsSnapshot     `json:"retry"`
	Transport OpenAIWSTransportMetricsSnapshot `json:"transport"`
}

func (s *OpenAIGatewayService) SnapshotOpenAIWSPerformanceMetrics() OpenAIWSPerformanceMetricsSnapshot {
	pool := s.getOpenAIWSConnPool()
	snapshot := OpenAIWSPerformanceMetricsSnapshot{
		Retry: s.SnapshotOpenAIWSRetryMetrics(),
	}
	if pool == nil {
		return snapshot
	}
	snapshot.Pool = pool.SnapshotMetrics()
	snapshot.Transport = pool.SnapshotTransportMetrics()
	return snapshot
}

func (s *OpenAIGatewayService) getOpenAIWSStateStore() OpenAIWSStateStore {
	if s == nil {
		return nil
	}
	s.openaiWSStateStoreOnce.Do(func() {
		if s.openaiWSStateStore == nil {
			s.openaiWSStateStore = NewOpenAIWSStateStore(s.cache)
		}
	})
	return s.openaiWSStateStore
}

func (s *OpenAIGatewayService) openAIWSResponseStickyTTL() time.Duration {
	if s != nil && s.cfg != nil {
		seconds := s.cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Hour
}

func (s *OpenAIGatewayService) openAIWSIngressPreviousResponseRecoveryEnabled() bool {
	if s != nil && s.cfg != nil {
		return s.cfg.Gateway.OpenAIWS.IngressPreviousResponseRecoveryEnabled
	}
	return true
}

func (s *OpenAIGatewayService) openAIWSReadTimeout() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.ReadTimeoutSeconds > 0 {
		return time.Duration(s.cfg.Gateway.OpenAIWS.ReadTimeoutSeconds) * time.Second
	}
	return 15 * time.Minute
}

func (s *OpenAIGatewayService) openAIWSPassthroughIdleTimeout() time.Duration {
	if timeout := s.openAIWSReadTimeout(); timeout > 0 {
		return timeout
	}
	return openAIWSPassthroughIdleTimeoutDefault
}

const openAIWSWriteTimeoutDefault = 2 * time.Minute

func (s *OpenAIGatewayService) openAIWSWriteTimeout() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.WriteTimeoutSeconds > 0 {
		return time.Duration(s.cfg.Gateway.OpenAIWS.WriteTimeoutSeconds) * time.Second
	}
	return openAIWSWriteTimeoutDefault
}

func (s *OpenAIGatewayService) openAIWSEventFlushBatchSize() int {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.EventFlushBatchSize > 0 {
		return s.cfg.Gateway.OpenAIWS.EventFlushBatchSize
	}
	return openAIWSEventFlushBatchSizeDefault
}

func (s *OpenAIGatewayService) openAIWSEventFlushInterval() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.EventFlushIntervalMS >= 0 {
		if s.cfg.Gateway.OpenAIWS.EventFlushIntervalMS == 0 {
			return 0
		}
		return time.Duration(s.cfg.Gateway.OpenAIWS.EventFlushIntervalMS) * time.Millisecond
	}
	return openAIWSEventFlushIntervalDefault
}

func (s *OpenAIGatewayService) openAIWSPayloadLogSampleRate() float64 {
	if s != nil && s.cfg != nil {
		rate := s.cfg.Gateway.OpenAIWS.PayloadLogSampleRate
		if rate < 0 {
			return 0
		}
		if rate > 1 {
			return 1
		}
		return rate
	}
	return openAIWSPayloadLogSampleDefault
}

func (s *OpenAIGatewayService) shouldLogOpenAIWSPayloadSchema(attempt int) bool {
	// 首次尝试保留一条完整 payload_schema 便于排障。
	if attempt <= 1 {
		return true
	}
	rate := s.openAIWSPayloadLogSampleRate()
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	return rand.Float64() < rate
}

func (s *OpenAIGatewayService) shouldEmitOpenAIWSPayloadSchema(attempt int) bool {
	if !s.shouldLogOpenAIWSPayloadSchema(attempt) {
		return false
	}
	return logger.L().Core().Enabled(zap.DebugLevel)
}

func (s *OpenAIGatewayService) openAIWSDialTimeout() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.DialTimeoutSeconds > 0 {
		return time.Duration(s.cfg.Gateway.OpenAIWS.DialTimeoutSeconds) * time.Second
	}
	return 10 * time.Second
}

func (s *OpenAIGatewayService) openAIWSAcquireTimeout() time.Duration {
	// Acquire 覆盖“连接复用命中/排队/新建连接”三个阶段。
	// 这里不再叠加 write_timeout，避免高并发排队时把 TTFT 长尾拉到分钟级。
	dial := s.openAIWSDialTimeout()
	if dial <= 0 {
		dial = 10 * time.Second
	}
	return dial + 2*time.Second
}

// forwardOpenAIWSV2WithRecovery 统一持有同账号 WS 重连，沿用请求的发送和等待总预算。
// 主转发器只决定协议与计费；已交付或已计量的结果不能进入传输重放。
func (s *OpenAIGatewayService) forwardOpenAIWSV2WithRecovery(
	ctx context.Context, c *gin.Context, account *Account, wsReqBody map[string]any,
	clientPromptCacheKey, wsExecutionScope, token string, wsDecision OpenAIWSProtocolDecision,
	isCodexCLI, reqStream bool, originalModel, upstreamModel string, startTime time.Time,
) (*OpenAIForwardResult, error, int, string) {
	maxAttempts := 1 + min(1, account.GetPoolModeRetryCount())
	wsAttempts := 0
	var wsResult *OpenAIForwardResult
	var wsErr error
	wsLastFailureReason := ""
	var uncertainExecutionFailure error
	agentTaskRecoveryTried := false
	retryBudget := s.openAIWSRetryTotalBudget()
	retryStartedAt := time.Now()
	var preparationDeadline time.Time
	if retryBudget > 0 {
		preparationDeadline = retryStartedAt.Add(retryBudget)
	}
wsRetryLoop:
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		wsAttempts = attempt
		wsResult, wsErr = s.forwardOpenAIWSV2(
			ctx,
			c,
			account,
			wsReqBody,
			clientPromptCacheKey,
			wsExecutionScope,
			token,
			wsDecision,
			isCodexCLI,
			reqStream,
			originalModel,
			upstreamModel,
			startTime,
			attempt,
			preparationDeadline,
			wsLastFailureReason,
			&agentTaskRecoveryTried,
		)
		if wsErr == nil {
			break
		}
		if wsResult != nil {
			break // Metered output or client disconnect cannot be replayed.
		}
		if openAIStreamClientOutputStarted(c, false) {
			break
		}
		if uncertainExecutionFailure != nil && ctx.Err() == nil && !IsTurnBudgetError(wsErr) {
			// A later preparation/auth failure cannot erase an earlier ambiguous
			// send. Preserve that fact for the existing replay/account policy.
			wsErr = uncertainExecutionFailure
			break
		}
		var rejectedGrant *openAIWSDialError
		if errors.As(wsErr, &rejectedGrant) && rejectedGrant.StatusCode == http.StatusUnauthorized && s.openAITokenProvider != nil && attempt < maxAttempts {
			if fresh, ok := refreshRejectedCredential(ctx, s.accountRepo, account, token, s.openAITokenProvider.refreshAPI, s.openAITokenProvider.executor); ok {
				token = fresh
				continue
			}
		}
		var taskRecoveredErr *agentIdentityTaskRecoveredError
		if errors.As(wsErr, &taskRecoveredErr) {
			continue
		}

		reason, retryable := classifyOpenAIWSReconnectReason(wsErr)
		if retryable && !openAIWSFailureBeforeSend(reason) {
			uncertainExecutionFailure = wsErr
		}
		if reason != "" {
			wsLastFailureReason = reason
		}
		if retryable && attempt < maxAttempts {
			backoff := s.openAIWSRetryBackoff(attempt)
			if retryBudget > 0 && time.Since(retryStartedAt)+backoff > retryBudget {
				s.recordOpenAIWSRetryExhausted()
				logOpenAIWSModeInfo(
					"reconnect_budget_exhausted account_id=%d attempts=%d max_retries=%d reason=%s elapsed_ms=%d budget_ms=%d",
					account.ID,
					attempt,
					openAIWSReconnectRetryLimit,
					normalizeOpenAIWSLogValue(reason),
					time.Since(retryStartedAt).Milliseconds(),
					retryBudget.Milliseconds(),
				)
				break
			}
			s.recordOpenAIWSRetryAttempt(backoff)
			logOpenAIWSModeInfo(
				"reconnect_retry account_id=%d retry=%d max_retries=%d reason=%s backoff_ms=%d",
				account.ID,
				attempt,
				openAIWSReconnectRetryLimit,
				normalizeOpenAIWSLogValue(reason),
				backoff.Milliseconds(),
			)
			if waitErr := WaitOpenAITurnRetry(ctx, backoff); waitErr != nil {
				wsErr = waitErr
				break wsRetryLoop
			}

			continue
		}
		if retryable {
			s.recordOpenAIWSRetryExhausted()
			logOpenAIWSModeInfo(
				"reconnect_exhausted account_id=%d attempts=%d max_retries=%d reason=%s",
				account.ID,
				attempt,
				openAIWSReconnectRetryLimit,
				normalizeOpenAIWSLogValue(reason),
			)
		} else if reason != "" {
			s.recordOpenAIWSNonRetryableFastFallback()
			logOpenAIWSModeInfo(
				"reconnect_stop account_id=%d attempt=%d reason=%s",
				account.ID,
				attempt,
				normalizeOpenAIWSLogValue(reason),
			)
		}
		break
	}
	return wsResult, wsErr, wsAttempts, token
}
