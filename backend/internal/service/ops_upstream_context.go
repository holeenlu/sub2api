package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	responseswire "github.com/Wei-Shaw/sub2api/internal/gateway/protocol/responses"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Gin context keys used by Ops error logger for capturing upstream error details.
// These keys are set by gateway services and consumed by handler/ops_error_logger.go.
const (
	OpsUpstreamStatusCodeKey   = "ops_upstream_status_code"
	OpsUpstreamErrorMessageKey = "ops_upstream_error_message"
	OpsUpstreamErrorDetailKey  = "ops_upstream_error_detail"
	OpsUpstreamErrorsKey       = "ops_upstream_errors"
	OpsUpstreamModelKey        = "ops_upstream_model"
	// A gateway-generated terminal must not overwrite the real HTTP attempt.
	OpsGatewayResponsesFailedKey = "ops_gateway_responses_failed"
	OpsLocalDependencyFailureKey = "ops_local_dependency_failure"

	// Optional stage latencies (milliseconds) for troubleshooting and alerting.
	OpsAuthLatencyMsKey      = "ops_auth_latency_ms"
	OpsRoutingLatencyMsKey   = "ops_routing_latency_ms"
	OpsUpstreamLatencyMsKey  = "ops_upstream_latency_ms"
	OpsResponseLatencyMsKey  = "ops_response_latency_ms"
	OpsTimeToFirstTokenMsKey = "ops_time_to_first_token_ms"
	// OpenAI WS 关键观测字段
	OpsOpenAIWSQueueWaitMsKey  = "ops_openai_ws_queue_wait_ms"
	OpsOpenAIWSConnPickMsKey   = "ops_openai_ws_conn_pick_ms"
	OpsOpenAIWSConnReusedKey   = "ops_openai_ws_conn_reused"
	OpsOpenAIWSConnIDKey       = "ops_openai_ws_conn_id"
	OpsOpenAIWSPayloadBytesKey = "ops_openai_ws_payload_bytes"

	// OpsSkipPassthroughKey 由 applyErrorPassthroughRule 在命中 skip_monitoring=true 的规则时设置。
	// ops_error_logger 中间件检查此 key，为 true 时跳过错误记录。
	OpsSkipPassthroughKey = "ops_skip_passthrough"

	// OpsStreamErrorKey 保存 handleStreamingAwareError 在「响应已固化为 HTTP 200 的 SSE 流」
	// 上就地(in-band)补发错误帧时记录的 OpsStreamError。因为 wire 状态码停留在 200，
	// ops_error_logger 的 status>=400 采集路径永远不会触发，这类流内失败
	//（例如等待并发槽位超时后回退的限流、Wait 后二次计费校验失败）本会在错误看板里隐形。
	OpsStreamErrorKey          = "ops_stream_error"
	OpsStreamErrorsKey         = "ops_stream_errors"
	OpsStreamTurnKey           = "ops_stream_turn"
	OpsStreamTurnSucceededKey  = "ops_stream_turn_succeeded"
	OpsStreamRecoveryLoggedKey = "ops_ws_recovery_logged"
	OpsStreamTurnStartedAtKey  = "ops_stream_turn_started_at"

	// Client-side configuration denials should remain visible in ops_error_logs,
	// but should be excluded from SLA/error-rate calculations.
	// ResponseCommittedKey 由 handleErrorResponse 系列函数在写完 HTTP 错误响应后设置。
	// ensureForwardErrorResponse 检查此 key，为 true 时跳过兜底写入，避免在已完成的 JSON 后追加 SSE。
	ResponseCommittedKey = "response_committed"

	// OpsLocalErrorTypeKey / OpsLocalErrorCodeKey 记录网关以 Google 错误格式回写的错误类型与错误码：
	// Google 错误体没有 type 字段，ops_error_logger 据此按与 Claude / OpenAI 格式错误相同的口径分类。
	OpsLocalErrorTypeKey = "ops_local_error_type"
	OpsLocalErrorCodeKey = "ops_local_error_code"
	// OpsRequestScopedErrorKey 标记失败由请求内容决定（如引用的显式缓存不存在）：只按错误类型分类，
	// 上游错误上下文保留为明细但不参与归因。
	OpsRequestScopedErrorKey = "ops_request_scoped_error"

	OpsClientBusinessLimitedKey                           = "ops_client_business_limited"
	OpsClientBusinessLimitedReasonKey                     = "ops_client_business_limited_reason"
	OpsClientBusinessLimitedReasonIPRestriction           = "api_key_ip_restriction"
	OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable  = "api_key_group_unavailable"
	OpsClientBusinessLimitedReasonAPIKeyGroupUnassigned   = "api_key_group_unassigned"
	OpsClientBusinessLimitedReasonLocalFeatureGate        = "local_feature_gate"
	OpsClientBusinessLimitedReasonLocalPolicyDenied       = "local_policy_denied"
	OpsClientBusinessLimitedReasonLocalModelConfiguration = "local_model_configuration"
)

func MarkResponseCommitted(c *gin.Context) { c.Set(ResponseCommittedKey, true) }

func IsResponseCommitted(c *gin.Context) bool {
	v, ok := c.Get(ResponseCommittedKey)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func SetOpsLatencyMs(c *gin.Context, key string, value int64) {
	if c == nil || strings.TrimSpace(key) == "" || value < 0 {
		return
	}
	c.Set(key, value)
	switch key {
	case OpsAuthLatencyMsKey:
		MarkOpenAIRequestTimingStage(c, "handler_ready")
	case OpsRoutingLatencyMsKey:
		MarkOpenAIRequestTimingStage(c, "routing_done")
	}
}

// GetOpsLatencyMs returns a detached value so recorded turns cannot inherit a
// later turn's timing. An unmeasured stage remains nil rather than zero.
func GetOpsLatencyMs(c *gin.Context, key string) *int64 {
	if c == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	v, ok := c.Get(key)
	if !ok {
		return nil
	}
	var ms int64
	switch t := v.(type) {
	case int:
		ms = int64(t)
	case int32:
		ms = int64(t)
	case int64:
		ms = t
	case float64:
		ms = int64(t)
	default:
		return nil
	}
	if ms < 0 {
		return nil
	}
	return &ms
}

// SetOpsUpstreamModel stores only the effective model slug for final Ops
// attribution. Call it immediately before an upstream attempt is dispatched.
func SetOpsUpstreamModel(c *gin.Context, model string) {
	if c == nil {
		return
	}
	if model = strings.TrimSpace(model); model != "" {
		c.Set(OpsUpstreamModelKey, model)
		if c.Request != nil {
			if b, _ := c.Request.Context().Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
				b.mu.Lock()
				b.model = model
				b.mu.Unlock()
			}
		}
	}
}

// ClearOpsUpstreamModel invalidates attempt-scoped model attribution before a
// newly selected account starts credential resolution or upstream dispatch.
func ClearOpsUpstreamModel(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(OpsUpstreamModelKey, "")
}

// MarkOpsLocalDependencyFailure records trusted gateway provenance without
// turning a service outage into a client/business rejection. Earlier attempts
// remain in the diagnostic timeline, but do not own this terminal failure.
func MarkOpsLocalDependencyFailure(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(OpsLocalDependencyFailureKey, true)
	c.Set(OpsGatewayResponsesFailedKey, true)
	c.Set(OpsUpstreamStatusCodeKey, 0)
	c.Set(OpsUpstreamErrorMessageKey, "")
	c.Set(OpsUpstreamErrorDetailKey, "")
}

func MarkOpsClientBusinessLimited(c *gin.Context, reason string) {
	if c == nil {
		return
	}
	c.Set(OpsClientBusinessLimitedKey, true)
	if reason = strings.TrimSpace(reason); reason != "" {
		c.Set(OpsClientBusinessLimitedReasonKey, reason)
	}
}

func HasOpsClientBusinessLimited(c *gin.Context) bool {
	if c == nil {
		return false
	}
	v, ok := c.Get(OpsClientBusinessLimitedKey)
	if !ok {
		return false
	}
	marked, _ := v.(bool)
	return marked
}

func OpsClientBusinessLimitedReason(c *gin.Context) string {
	if c == nil {
		return ""
	}
	v, ok := c.Get(OpsClientBusinessLimitedReasonKey)
	if !ok {
		return ""
	}
	reason, _ := v.(string)
	return strings.TrimSpace(reason)
}

// SetOpsLocalErrorType 登记响应错误体缺少的错误类型与错误码，并清除请求级失败标记；errType 为空表示不登记。
func SetOpsLocalErrorType(c *gin.Context, errType, code string) {
	if c == nil {
		return
	}
	c.Set(OpsLocalErrorTypeKey, strings.TrimSpace(errType))
	c.Set(OpsLocalErrorCodeKey, strings.TrimSpace(code))
	c.Set(OpsRequestScopedErrorKey, false)
}

// OpsLocalErrorType 返回 SetOpsLocalErrorType 登记的错误类型与错误码；未登记时返回空串。
func OpsLocalErrorType(c *gin.Context) (errType, code string) {
	if c == nil {
		return "", ""
	}
	errType = c.GetString(OpsLocalErrorTypeKey)
	if errType == "" {
		return "", ""
	}
	return errType, c.GetString(OpsLocalErrorCodeKey)
}

// MarkOpsRequestScopedError 登记错误类型，并标记失败由请求内容决定：上游错误上下文不参与 ops 归因。
func MarkOpsRequestScopedError(c *gin.Context, errType string) {
	if c == nil {
		return
	}
	SetOpsLocalErrorType(c, errType, "")
	c.Set(OpsRequestScopedErrorKey, true)
}

// IsOpsRequestScopedError 判断 MarkOpsRequestScopedError 是否标记了本次请求的失败。
func IsOpsRequestScopedError(c *gin.Context) bool {
	return c != nil && c.GetBool(OpsRequestScopedErrorKey)
}

// OpsStreamError 描述承载在 2xx 响应上的带内错误：网关在响应状态已固化为 200 之后
// 就地以 SSE error 帧返回的错误（并发限流回退、Wait 后二次计费校验失败、流开始后才无
// 可用账号等），以及上游 2xx 正文或事件里携带的错误结果（NonStream 标记非流式正文）。
// 由于 HTTP 状态码停留在 2xx，ops_error_logger 中间件在 status<400 分支消费该标记并
// 补记错误日志；标记方是 handler.handleStreamingAwareError 或各 service 的带内检测。
type OpsStreamError struct {
	// ErrType 是写入 SSE 帧的对客错误类型（如 rate_limit_error / upstream_error / api_error）。
	ErrType string
	// Code 是可选的稳定错误分类；用于既保留通用 OpenAI error.type，又向客户端和 Ops
	// 暴露可编程判断的细分类（如 upstream_http2_stream_error）。
	Code string
	// Message 是写入 SSE 帧的对客错误消息。
	Message string
	// IntendedStatus 是流若未固化本应返回的 HTTP 状态码（如并发限流的 429）。
	// 默认仅用于错误分级；CountTowardsSLA 或 RequestScoped 为 true 时也作为 Ops 的逻辑状态码。
	IntendedStatus int
	// CountTowardsSLA 表示虽然 wire 状态已固化为 200，请求在应用语义上仍然失败，
	// Ops 应使用 IntendedStatus 计入错误率/SLA。
	CountTowardsSLA bool
	// Turn identifies a WebSocket turn. HTTP/SSE requests leave it at zero.
	Turn int
	// SkipMonitoring snapshots the rule decision for this visible failure.
	SkipMonitoring         bool
	LocalDependencyFailure bool
	AccountID              int64
	UpstreamModel          string
	UpstreamStatus         int
	UpstreamMessage        string
	UpstreamDetail         string
	UpstreamErrors         []*OpsUpstreamErrorEvent
	// Latencies are frozen when the failure is recorded, including local and
	// request-scoped failures; the connection may keep serving later turns.
	AuthLatencyMs      *int64
	RoutingLatencyMs   *int64
	UpstreamLatencyMs  *int64
	ResponseLatencyMs  *int64
	TimeToFirstTokenMs *int64
	// RequestScoped 表示该带内失败是请求级结果（如上游内容策略截停），与本请求此前的
	// 上游尝试无关：分类不受上游错误上下文影响、不快照也不落库上游归因、不继承透传规则的
	// skip_monitoring，按业务限制计，落库状态取 IntendedStatus 以便进入错误列表。
	RequestScoped bool
	// NonStream 表示带内信号来自非流式 2xx 响应体，落库 stream=false。
	NonStream bool
}

const maxOpsStreamErrorsPerRequest = 64

// BeginOpsStreamTurn scopes first-wins deduplication to one WebSocket turn.
func BeginOpsStreamTurn(c *gin.Context, turn int, receivedAt ...time.Time) {
	if c == nil || turn <= 0 {
		return
	}
	resetOpenAITurnTiming(c, turn, receivedAt...)
	if turn > 1 {
		// Keep the initial HTTP authentication/selection timing on the first
		// turn, but never carry any measured stage into the next response.
		for _, key := range []string{OpsAuthLatencyMsKey, OpsRoutingLatencyMsKey, OpsUpstreamLatencyMsKey, OpsResponseLatencyMsKey, OpsTimeToFirstTokenMsKey} {
			c.Set(key, nil)
		}
	}
	if c.Request != nil {
		LogSchedulingTrace(c.Request.Context(), HasOpsCurrentStreamError(c))
		resetSchedulingTrace(c.Request.Context())
	}
	ClearOpsUpstreamModel(c)
	c.Set(OpsStreamTurnKey, turn)
	c.Set(OpsStreamTurnSucceededKey, false)
	c.Set(OpsStreamRecoveryLoggedKey, false)
	c.Set(OpsStreamTurnStartedAtKey, time.Now())
	c.Set(openAIResponseProgressContextKey, (*openAIResponseProgress)(nil))
	c.Set(openAIReplayBlockerContextKey, "")
	c.Set(openAIReplayUnresolvedContextKey, "")
	c.Set(openAIAccountReplayBlockerContextKey, "")
	c.Set(OpenAIAccountReplayAllowedContextKey, nil)
	// Rule and attempt state is turn-scoped on a long-lived WS connection.
	c.Set(OpsSkipPassthroughKey, false)
	c.Set(OpsGatewayResponsesFailedKey, false)
	c.Set(OpsLocalDependencyFailureKey, false)
	c.Set(OpsUpstreamErrorsKey, []*OpsUpstreamErrorEvent{})
	c.Set(OpsUpstreamStatusCodeKey, 0)
	c.Set(OpsUpstreamErrorMessageKey, "")
	c.Set(OpsUpstreamErrorDetailKey, "")
}

// MarkOpsStreamError 记录一次就地 SSE 错误，供 ops 日志采集。
// 采用「首个标记生效」策略：同一请求若先后补发多帧（如上游透传错误后又追加通用兜底帧），
// 保留最先记录的根因错误，而不是被后续的 "Upstream request failed" 覆盖。
func MarkOpsStreamError(c *gin.Context, errType, message string, intendedStatus int) {
	markOpsStreamError(c, OpsStreamError{
		ErrType:        errType,
		Message:        message,
		IntendedStatus: intendedStatus,
	})
}

// MarkOpsStreamFailure records an in-band stream error that represents a failed
// request and therefore must count towards Ops error rate/SLA despite HTTP 200
// already being committed on the wire.
func MarkOpsStreamFailure(c *gin.Context, errType, code, message string, intendedStatus int) {
	markOpsStreamError(c, OpsStreamError{
		ErrType:         errType,
		Code:            code,
		Message:         message,
		IntendedStatus:  intendedStatus,
		CountTowardsSLA: true,
	})
}

// MarkOpsStreamErrorValue 以完整的 OpsStreamError 记录一次带内错误，供需要
// RequestScoped / NonStream 等附加语义的调用方使用；首个标记生效的规则不变。
// 调用方只填 ErrType / Code / Message / IntendedStatus / CountTowardsSLA / RequestScoped / NonStream。
// AccountID、UpstreamModel 与 Turn 由请求上下文接管；UpstreamStatus、UpstreamMessage、
// UpstreamDetail、UpstreamErrors 与 SkipMonitoring 在非 RequestScoped 时由上下文接管，
// RequestScoped 时被忽略。
func MarkOpsStreamErrorValue(c *gin.Context, streamErr OpsStreamError) {
	markOpsStreamError(c, streamErr)
}

func markOpsStreamError(c *gin.Context, streamErr OpsStreamError) {
	if c == nil {
		return
	}
	streamErr.ErrType = strings.TrimSpace(streamErr.ErrType)
	streamErr.Code = strings.TrimSpace(streamErr.Code)
	streamErr.Message = strings.TrimSpace(streamErr.Message)
	if !streamErr.RequestScoped {
		streamErr.SkipMonitoring = currentOpsFailureSkipMonitoring(c)
	}
	snapshotOpsStreamErrorContext(c, &streamErr)
	if GetOpenAIClientTransport(c) == OpenAIClientTransportWS {
		if value, ok := c.Get(OpsStreamTurnKey); ok {
			streamErr.Turn, _ = value.(int)
		}
		var errorsForRequest []OpsStreamError
		if value, ok := c.Get(OpsStreamErrorsKey); ok {
			errorsForRequest, _ = value.([]OpsStreamError)
		}
		if len(errorsForRequest) > 0 && errorsForRequest[len(errorsForRequest)-1].Turn == streamErr.Turn {
			return
		}
		errorsForRequest = append(errorsForRequest, streamErr)
		if len(errorsForRequest) > maxOpsStreamErrorsPerRequest {
			errorsForRequest = append([]OpsStreamError(nil), errorsForRequest[len(errorsForRequest)-maxOpsStreamErrorsPerRequest:]...)
		}
		c.Set(OpsStreamErrorsKey, errorsForRequest)
		c.Set(OpsStreamErrorKey, streamErr)
		return
	}
	if _, exists := c.Get(OpsStreamErrorKey); exists {
		return
	}
	c.Set(OpsStreamErrorKey, streamErr)
}

func snapshotOpsStreamErrorContext(c *gin.Context, streamErr *OpsStreamError) {
	if c == nil || streamErr == nil {
		return
	}
	streamErr.LocalDependencyFailure = c.GetBool(OpsLocalDependencyFailureKey)
	streamErr.AuthLatencyMs = GetOpsLatencyMs(c, OpsAuthLatencyMsKey)
	streamErr.RoutingLatencyMs = GetOpsLatencyMs(c, OpsRoutingLatencyMsKey)
	streamErr.UpstreamLatencyMs = GetOpsLatencyMs(c, OpsUpstreamLatencyMsKey)
	streamErr.ResponseLatencyMs = GetOpsLatencyMs(c, OpsResponseLatencyMsKey)
	streamErr.TimeToFirstTokenMs = GetOpsLatencyMs(c, OpsTimeToFirstTokenMsKey)
	if c.Request != nil {
		if accountID, ok := c.Request.Context().Value(ctxkey.AccountID).(int64); ok && accountID > 0 {
			streamErr.AccountID = accountID
		}
	}
	if value, ok := c.Get(OpsUpstreamModelKey); ok {
		streamErr.UpstreamModel, _ = value.(string)
		streamErr.UpstreamModel = strings.TrimSpace(streamErr.UpstreamModel)
	}
	if streamErr.RequestScoped {
		streamErr.UpstreamErrors = nil
		if c.Request != nil {
			streamErr.UpstreamErrors = WithOpsSchedulingTrace(c.Request.Context(), nil)
		}
		return
	}
	if value, ok := c.Get(OpsUpstreamStatusCodeKey); ok {
		switch status := value.(type) {
		case int:
			streamErr.UpstreamStatus = status
		case int64:
			streamErr.UpstreamStatus = int(status)
		}
	}
	if value, ok := c.Get(OpsUpstreamErrorMessageKey); ok {
		streamErr.UpstreamMessage, _ = value.(string)
		streamErr.UpstreamMessage = strings.TrimSpace(streamErr.UpstreamMessage)
	}
	if value, ok := c.Get(OpsUpstreamErrorDetailKey); ok {
		streamErr.UpstreamDetail, _ = value.(string)
		streamErr.UpstreamDetail = strings.TrimSpace(streamErr.UpstreamDetail)
	}
	if value, ok := c.Get(OpsUpstreamErrorsKey); ok {
		if events, ok := value.([]*OpsUpstreamErrorEvent); ok {
			streamErr.UpstreamErrors = make([]*OpsUpstreamErrorEvent, 0, len(events))
			for _, event := range events {
				if event == nil {
					streamErr.UpstreamErrors = append(streamErr.UpstreamErrors, nil)
					continue
				}
				copyOfEvent := *event
				streamErr.UpstreamErrors = append(streamErr.UpstreamErrors, &copyOfEvent)
			}
		}
	}
	if c.Request != nil {
		streamErr.UpstreamErrors = WithOpsSchedulingTrace(c.Request.Context(), streamErr.UpstreamErrors)
	}
}

func currentOpsFailureSkipMonitoring(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if value, ok := c.Get(OpsSkipPassthroughKey); ok {
		if skip, _ := value.(bool); skip {
			return true
		}
	}
	if value, ok := c.Get(OpsUpstreamErrorsKey); ok {
		if events, ok := value.([]*OpsUpstreamErrorEvent); ok {
			if last := LastOpsUpstreamAttempt(events); last != nil {
				return last.SkipMonitoring
			}
		}
	}
	return false
}

// GetOpsStreamError 返回本请求记录的就地 SSE 错误（若有）。
func GetOpsStreamError(c *gin.Context) (OpsStreamError, bool) {
	if c == nil {
		return OpsStreamError{}, false
	}
	v, ok := c.Get(OpsStreamErrorKey)
	if !ok {
		return OpsStreamError{}, false
	}
	se, ok := v.(OpsStreamError)
	return se, ok
}

// HasOpsCurrentStreamError excludes earlier failures on a long-lived WS connection.
func HasOpsCurrentStreamError(c *gin.Context) bool {
	err, ok := GetOpsStreamError(c)
	return ok && err.Turn == c.GetInt(OpsStreamTurnKey)
}

func GetOpsStreamErrors(c *gin.Context) []OpsStreamError {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(OpsStreamErrorsKey); ok {
		if errorsForRequest, ok := value.([]OpsStreamError); ok && len(errorsForRequest) > 0 {
			return append([]OpsStreamError(nil), errorsForRequest...)
		}
	}
	if streamErr, ok := GetOpsStreamError(c); ok {
		return []OpsStreamError{streamErr}
	}
	return nil
}

// SetOpsUpstreamError is the exported wrapper for setOpsUpstreamError, used by
// handler-layer code (e.g. failover-exhausted paths) that needs to record the
// original upstream status code before mapping it to a client-facing code.
func SetOpsUpstreamError(c *gin.Context, upstreamStatusCode int, upstreamMessage, upstreamDetail string) {
	setOpsUpstreamError(c, upstreamStatusCode, upstreamMessage, upstreamDetail)
}

func setOpsUpstreamError(c *gin.Context, upstreamStatusCode int, upstreamMessage, upstreamDetail string) {
	if c == nil {
		return
	}
	if upstreamStatusCode > 0 {
		c.Set(OpsUpstreamStatusCodeKey, upstreamStatusCode)
	}
	if msg := strings.TrimSpace(upstreamMessage); msg != "" {
		c.Set(OpsUpstreamErrorMessageKey, msg)
	}
	if detail := strings.TrimSpace(upstreamDetail); detail != "" {
		c.Set(OpsUpstreamErrorDetailKey, detail)
	}
}

// OpsUpstreamErrorEvent describes an upstream attempt or a local diagnostic event during a gateway request.
// It is stored in ops_error_logs.upstream_errors as a JSON array.
type OpsUpstreamErrorEvent struct {
	ConversionDiagnostics apicompat.ConversionDiagnostics `json:"conversion_diagnostics,omitempty"`
	AtUnixMs              int64                           `json:"at_unix_ms,omitempty"`

	// Passthrough 表示本次请求是否命中“原样透传（仅替换认证）”分支。
	// 该字段用于排障与灰度评估；存入 JSON，不涉及 DB schema 变更。
	Passthrough bool `json:"passthrough,omitempty"`

	// Context
	Platform    string `json:"platform,omitempty"`
	AccountID   int64  `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`

	// Proxy attribution is an immutable, credential-free snapshot of the route
	// used by this attempt. ProxyID is null for direct and unknown routes;
	// ProxyName distinguishes direct/no_proxy from unknown.
	// Never add proxy URLs or credentials to this event.
	ProxyID   *int64 `json:"proxy_id"`
	ProxyName string `json:"proxy_name"`

	// DroppedEarlierAttempts is set on the oldest retained event when queue
	// bounds forced earlier attempts of the same request to be discarded.
	DroppedEarlierAttempts int `json:"dropped_earlier_attempts,omitempty"`

	// Outcome
	UpstreamStatusCode int    `json:"upstream_status_code,omitempty"`
	UpstreamRequestID  string `json:"upstream_request_id,omitempty"`

	// UpstreamURL is the actual upstream URL that was called (host + path, query/fragment stripped).
	// Helps debug 404/routing errors by showing which endpoint was targeted.
	UpstreamURL string `json:"upstream_url,omitempty"`

	// Best-effort upstream response capture (sanitized+trimmed).
	UpstreamResponseBody string `json:"upstream_response_body,omitempty"`

	// Kind: http_error | request_error | retry_exhausted | failover | retry_stopped | scheduling_trace
	Kind string `json:"kind,omitempty"`
	// Stage/Scope/Reason distinguish credential acquisition from inference
	// without overloading upstream_status_code with a synthetic HTTP status.
	Stage  string `json:"stage,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Reason string `json:"reason,omitempty"`

	Message string `json:"message,omitempty"`
	Detail  string `json:"detail,omitempty"`

	// SkipMonitoring is request-local rule state. It is intentionally excluded
	// from persisted attempt JSON. The logger consults it only when this event is
	// the final client-visible failure; recovered attempts remain provider-health
	// telemetry and do not count as failed requests.
	SkipMonitoring bool `json:"-"`
}

// Retry-stop events describe a gateway decision, not another upstream attempt.
const OpsUpstreamRetryStopped = "retry_stopped"

const openAIResponseProgressContextKey = "openai_response_progress"

// Owned by the current response reader/writer. Only immutable, bounded JSON
// snapshots enter Ops; input, output, tool names and ciphertext are excluded.
type openAIResponseProgress struct {
	mode, protocol, commitReason          string
	firstSemanticEvent, firstSemanticItem string
	visibleSeen                           bool
	upstreamCloseCode                     int
	createdAt                             time.Time
	committed, semanticSeen               bool
	countsKnown                           bool
	events, metadataEvents, tokenEvents   int
	state                                 func() (bool, bool)
}

func beginOpenAIResponseProgress(c *gin.Context, mode, protocol string, state func() (bool, bool)) *openAIResponseProgress {
	p := &openAIResponseProgress{mode: mode, protocol: protocol, state: state}
	if c != nil {
		c.Set(openAIResponseProgressContextKey, p)
	}
	return p
}

func (p *openAIResponseProgress) observeUpstream(eventType string) {
	if eventType == "response.created" && p.createdAt.IsZero() {
		p.createdAt = time.Now()
	}
}

func (p *openAIResponseProgress) observeDownstream(payload []byte) {
	typ := gjson.GetBytes(payload, "type").String()
	p.countsKnown = true
	p.events++
	if responseswire.IsTurnMetadataEvent(typ) || typ == "response.created" || typ == "response.in_progress" {
		p.metadataEvents++
	}
	if responseswire.IsTokenEvent(typ) {
		p.tokenEvents++
	}
	if typ != "keepalive" {
		p.committed = true
	}
	if typ != "error" && !responseswire.IsTerminalEvent(typ) && openAIStreamDataStartsClientOutput(string(payload), typ) {
		p.semanticSeen = true
		if p.firstSemanticEvent == "" {
			p.firstSemanticEvent, p.firstSemanticItem = openAIProgressEventLabels(payload, typ)
		}
	}
	if !p.visibleSeen && responseswire.StartsVisibleOutput(string(payload), typ) {
		p.visibleSeen = true
	}
}

// Diagnostic labels only: recovery and TTFT still use their existing classifiers.
// Unknown provider-controlled names collapse to a fixed label, never raw content.
func openAIProgressEventLabels(payload []byte, typ string) (event, item string) {
	switch typ {
	case "response.output_item.added", "response.output_item.done",
		"response.content_part.added", "response.content_part.done",
		"response.reasoning_summary_part.added", "response.reasoning_summary_part.done",
		"response.reasoning_summary_text.done", "response.reasoning_text.done",
		"response.function_call_arguments.done", "response.custom_tool_call_input.done",
		"response.web_search_call.in_progress", "response.web_search_call.searching", "response.web_search_call.completed",
		"response.file_search_call.in_progress", "response.file_search_call.searching", "response.file_search_call.completed",
		"response.code_interpreter_call.in_progress", "response.code_interpreter_call.interpreting", "response.code_interpreter_call.completed",
		"response.mcp_call.in_progress", "response.mcp_call.completed", "response.mcp_call.failed",
		"response.image_generation_call.in_progress", "response.image_generation_call.generating", "response.image_generation_call.completed":
		event = typ
	default:
		event = "other_event"
		if responseswire.IsTokenEvent(typ) {
			event = "token_event"
		}
	}
	if typ == "response.output_item.added" || typ == "response.output_item.done" {
		item = gjson.GetBytes(payload, "item.type").String()
		switch item {
		case "message", "reasoning", "compaction", "function_call", "custom_tool_call",
			"web_search_call", "file_search_call", "mcp_call", "mcp_list_tools", "mcp_approval_request",
			"code_interpreter_call", "image_generation_call", "computer_call", "local_shell_call", "shell_call", "apply_patch_call":
		default:
			item = "other_item"
		}
	}
	return event, item
}

func (p *openAIResponseProgress) details() map[string]any {
	committed, semantic := p.committed, p.semanticSeen
	if p.state != nil {
		committed, semantic = p.state()
	}
	createdMS := int64(-1)
	if !p.createdAt.IsZero() {
		createdMS = time.Since(p.createdAt).Milliseconds()
	}
	data := map[string]any{"upstream_mode": p.mode, "created_to_failure_ms": createdMS, "downstream_committed": committed, "semantic_output_seen": semantic}
	if p.protocol != "" {
		data["upstream_protocol"] = p.protocol
	}
	if p.commitReason != "" {
		data["commit_reason"] = p.commitReason
	}
	if p.firstSemanticEvent != "" {
		data["first_semantic_event"] = p.firstSemanticEvent
	}
	if p.firstSemanticItem != "" {
		data["first_semantic_item_type"] = p.firstSemanticItem
	}
	if p.upstreamCloseCode > 0 {
		data["upstream_close_code"] = p.upstreamCloseCode
	}
	if p.countsKnown {
		data["visible_output_seen"] = p.visibleSeen
		data["downstream_events"], data["downstream_metadata_events"], data["downstream_token_events"] = p.events, p.metadataEvents, p.tokenEvents
	}
	return data
}

type OpenAIRetryStopReason string

const (
	OpenAIRetryStopDownstreamCommitted OpenAIRetryStopReason = "downstream_committed"
	OpenAIRetryStopReplayUnsafe        OpenAIRetryStopReason = "replay_unsafe"
	OpenAIRetryStopContentRefusal      OpenAIRetryStopReason = "content_refusal"
	OpenAIRetryStopTurnBudget          OpenAIRetryStopReason = "turn_budget"
	OpenAIRetryStopAccountSwitchLimit  OpenAIRetryStopReason = "account_switch_limit"
	OpenAIRetryStopNoAvailableAccount  OpenAIRetryStopReason = "no_available_account"
	OpenAIRetryStopContextNotPortable  OpenAIRetryStopReason = "context_not_portable"
	OpenAIRetryStopHistoryIncomplete   OpenAIRetryStopReason = "history_incomplete"
)

// LastOpsUpstreamAttempt excludes local decisions from attribution and monitoring
// rules while retaining those decisions in the persisted event array. A local
// protocol switch is not evidence of another upstream attempt or failure.
func LastOpsUpstreamAttempt(events []*OpsUpstreamErrorEvent) *OpsUpstreamErrorEvent {
	for i := len(events) - 1; i >= 0; i-- {
		if event := events[i]; event != nil && event.Kind == "bridge_error" {
			return nil // A terminal local conversion failure owns this outcome.
		}
		if event := events[i]; event != nil && event.Kind != OpsUpstreamRetryStopped && event.Kind != opsSchedulingTrace && event.Kind != "recovery" && event.Kind != cloudAdaptationKind && event.Kind != cloudFailureKind {
			return event
		}
	}
	return nil
}

// RecordOpenAIRetryStop records one final decision per HTTP request or WS turn.
// Call only when recovery has ended, not while a bare error remains provisional.
func RecordOpenAIRetryStop(c *gin.Context, account *Account, reason OpenAIRetryStopReason, details ...string) {
	if c == nil || (c.Request != nil && c.Request.Context().Err() != nil) {
		return
	}
	switch reason {
	case OpenAIRetryStopDownstreamCommitted, OpenAIRetryStopReplayUnsafe, OpenAIRetryStopContentRefusal,
		OpenAIRetryStopTurnBudget, OpenAIRetryStopAccountSwitchLimit, OpenAIRetryStopNoAvailableAccount,
		OpenAIRetryStopContextNotPortable, OpenAIRetryStopHistoryIncomplete:
	default:
		return
	}
	if value, ok := c.Get(OpsUpstreamErrorsKey); ok {
		if events, ok := value.([]*OpsUpstreamErrorEvent); ok {
			for _, event := range events {
				if event != nil && event.Kind == OpsUpstreamRetryStopped {
					return
				}
			}
		}
	}
	event := OpsUpstreamErrorEvent{
		Kind: OpsUpstreamRetryStopped, Stage: "recovery", Scope: string(GatewayFailureScopeRequest),
		Reason: string(reason), Platform: PlatformOpenAI,
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
	}
	data := map[string]any{}
	if value, ok := c.Get(openAIResponseProgressContextKey); ok {
		if progress, ok := value.(*openAIResponseProgress); ok && progress != nil {
			data = progress.details()
		}
	}
	if len(details) > 0 && details[0] != "" {
		var snapshot map[string]any
		if json.Unmarshal([]byte(details[0]), &snapshot) == nil {
			for key, value := range snapshot {
				data[key] = value
			}
		}
	}
	if transport := GetOpenAIClientTransport(c); transport != "" {
		data["ingress_transport"] = string(transport)
	}
	if blocker := c.GetString(openAIReplayBlockerContextKey); blocker != "" {
		data["blocking_tool_kind"] = blocker
	}
	if unresolved := c.GetString(openAIReplayUnresolvedContextKey); unresolved != "" {
		data["unresolved_context"] = unresolved
	}
	if blocker := c.GetString(openAIAccountReplayBlockerContextKey); blocker != "" {
		data["context_blocker"] = blocker
	}
	if portable, known := c.Get(OpenAIAccountReplayAllowedContextKey); known {
		if value, ok := portable.(bool); ok {
			data["context_portable"] = value
		}
	}
	if c.Request != nil {
		if budget, ok := c.Request.Context().Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); ok && budget != nil {
			budget.mu.Lock()
			attempts := budget.attemptBudget.Snapshot()
			data["attempts"] = attempts.Attempts
			data["attempt_limit"] = attempts.Limit
			data["credential_owners"] = attempts.Owners
			data["preparation_retries"] = attempts.PreparationRetries
			data["retry_wait_ms"] = attempts.RetryWait.Milliseconds()
			data["selection_operations"] = budget.selection.Operations()
			if deadline := budget.firstOutput.Snapshot().Deadline; !deadline.IsZero() {
				remaining := time.Until(deadline).Milliseconds()
				if remaining < 0 {
					remaining = 0
				}
				data["budget_remaining_ms"] = remaining
			}
			budget.mu.Unlock()
		}
	}
	if len(data) > 0 {
		encoded, _ := json.Marshal(data)
		event.Detail = truncateString(string(encoded), 1024)
	}
	if account != nil {
		event.AccountID, event.AccountName = account.ID, account.Name
		if account.Platform != "" {
			event.Platform = account.Platform
		}
	} else if value, ok := c.Get(OpsUpstreamErrorsKey); ok {
		if events, ok := value.([]*OpsUpstreamErrorEvent); ok {
			if last := LastOpsUpstreamAttempt(events); last != nil {
				event.Platform, event.AccountID, event.AccountName = last.Platform, last.AccountID, last.AccountName
				event.ProxyID, event.ProxyName = last.ProxyID, last.ProxyName
			}
		}
	}
	// No upstream status/message: this event must not mask the actual failure,
	// match provider passthrough rules or count as an extra account switch.
	appendOpsUpstreamError(c, event)
}

const (
	opsProxyNameDirect  = "direct/no_proxy"
	opsProxyNameUnknown = "unknown"
	// opsProxyNameUnnamed labels a managed proxy whose name is blank. The
	// proxies.name column is NOT NULL/non-empty, so this is a defensive value.
	opsProxyNameUnnamed = "proxy"
)

func appendOpsUpstreamError(c *gin.Context, ev OpsUpstreamErrorEvent) {
	if c == nil {
		return
	}
	if ev.AtUnixMs <= 0 {
		ev.AtUnixMs = time.Now().UnixMilli()
	}
	ev.Platform = strings.TrimSpace(ev.Platform)
	normalizeOpsUpstreamProxyAttribution(&ev)
	ev.UpstreamRequestID = strings.TrimSpace(ev.UpstreamRequestID)
	ev.UpstreamResponseBody = strings.TrimSpace(ev.UpstreamResponseBody)
	ev.Kind = strings.TrimSpace(ev.Kind)
	ev.Stage = strings.TrimSpace(ev.Stage)
	ev.Scope = strings.TrimSpace(ev.Scope)
	ev.Reason = strings.TrimSpace(ev.Reason)
	ev.UpstreamURL = strings.TrimSpace(ev.UpstreamURL)
	ev.Message = strings.TrimSpace(ev.Message)
	ev.Detail = strings.TrimSpace(ev.Detail)
	if ev.Message != "" {
		ev.Message = sanitizeUpstreamErrorMessage(ev.Message)
	}

	var existing []*OpsUpstreamErrorEvent
	if v, ok := c.Get(OpsUpstreamErrorsKey); ok {
		if arr, ok := v.([]*OpsUpstreamErrorEvent); ok {
			existing = arr
		}
	}

	if value, ok := c.Get(conversionDiagnosticsContextKey); ok {
		if diagnostics, ok := value.(apicompat.ConversionDiagnostics); ok {
			for _, item := range diagnostics {
				ev.ConversionDiagnostics.Add(item.Code, item.Path)
			}
		}
	}
	evCopy := ev
	existing = append(existing, &evCopy)
	c.Set(OpsUpstreamErrorsKey, existing)

	checkSkipMonitoringForUpstreamEvent(c, &evCopy)
}

// opsUpstreamProxyAttribution derives both attribution fields from one
// decision so they can never disagree. The event is assembled at the failure
// site; forwarding code builds managed proxy routes from Proxy only when the
// binding ID is also set, so the same rule decides the label here:
//
//   - nil account                     -> (nil, unknown)
//   - no binding or no hydrated Proxy -> (nil, direct/no_proxy)
//   - hydrated Proxy without durable ID -> (nil, unknown): the transport did
//     use that proxy, but nothing durable identifies it
//   - otherwise                       -> (Proxy.ID, Proxy.Name or "proxy")
//
// Invariant: proxy_id == null implies proxy_name is one of the two sentinels.
func opsUpstreamProxyAttribution(account *Account) (*int64, string) {
	if account == nil {
		return nil, opsProxyNameUnknown
	}
	if account.ProxyID == nil || account.Proxy == nil {
		return nil, opsProxyNameDirect
	}
	if account.Proxy.ID <= 0 {
		return nil, opsProxyNameUnknown
	}
	proxyID := account.Proxy.ID
	name := strings.TrimSpace(account.Proxy.Name)
	if name == "" {
		name = opsProxyNameUnnamed
	}
	return &proxyID, name
}

func opsUpstreamProxyID(account *Account) *int64 {
	proxyID, _ := opsUpstreamProxyAttribution(account)
	return proxyID
}

func opsUpstreamProxyName(account *Account) string {
	_, name := opsUpstreamProxyAttribution(account)
	return name
}

// opsUpstreamWSProxyAttribution is the OpenAI WebSocket variant. The WS dialer
// sets an explicit proxy client only when the account has a usable managed
// proxy; otherwise coder/websocket falls back to http.DefaultClient, which
// honors HTTP_PROXY/HTTPS_PROXY/NO_PROXY. A missing managed proxy is therefore
// unknown, not direct.
func opsUpstreamWSProxyAttribution(account *Account) (*int64, string) {
	proxyID, name := opsUpstreamProxyAttribution(account)
	if proxyID == nil {
		return nil, opsProxyNameUnknown
	}
	return proxyID, name
}

func setUnknownOpsUpstreamProxy(ev *OpsUpstreamErrorEvent) {
	if ev == nil {
		return
	}
	ev.ProxyID = nil
	ev.ProxyName = opsProxyNameUnknown
}

// normalizeOpsUpstreamProxyAttribution makes legacy events explicit without
// pretending that the account's current proxy is historical evidence.
func normalizeOpsUpstreamProxyAttribution(ev *OpsUpstreamErrorEvent) {
	if ev == nil {
		return
	}
	if ev.ProxyID != nil && *ev.ProxyID <= 0 {
		// A non-positive ID never identifies a managed proxy.
		ev.ProxyID = nil
	}
	ev.ProxyName = strings.TrimSpace(ev.ProxyName)
	if ev.ProxyID != nil {
		if ev.ProxyName == "" {
			ev.ProxyName = opsProxyNameUnnamed
		}
		return
	}
	// Invariant: proxy_id == null implies a sentinel name. Any other name
	// without a durable ID is not historical evidence of a managed route.
	if ev.ProxyName != opsProxyNameDirect {
		setUnknownOpsUpstreamProxy(ev)
	}
}

// checkSkipMonitoringForUpstreamEvent snapshots whether this attempt matches a
// skip_monitoring passthrough rule. The final failure decides whether the
// request error is hidden; an intermediate recovered attempt cannot suppress a
// later client-visible failure.
func checkSkipMonitoringForUpstreamEvent(c *gin.Context, ev *OpsUpstreamErrorEvent) {
	if ev.UpstreamStatusCode == 0 {
		return
	}

	svc := getBoundErrorPassthroughService(c)
	if svc == nil {
		return
	}

	// Use the best available body representation for keyword matching.
	// Even when body is empty, MatchRule can still match rules that only
	// specify ErrorCodes (no Keywords), so we always call it.
	body := ev.Detail
	if body == "" {
		body = ev.Message
	}

	rule := svc.MatchRule(ev.Platform, ev.UpstreamStatusCode, []byte(body))
	if rule != nil && rule.SkipMonitoring {
		ev.SkipMonitoring = true
	}
}

func marshalOpsUpstreamErrors(events []*OpsUpstreamErrorEvent) *string {
	if len(events) == 0 {
		return nil
	}
	// Ensure we always store a valid JSON value.
	raw, err := json.Marshal(events)
	if err != nil || len(raw) == 0 {
		return nil
	}
	s := string(raw)
	return &s
}

func ParseOpsUpstreamErrors(raw string) ([]*OpsUpstreamErrorEvent, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []*OpsUpstreamErrorEvent{}, nil
	}
	var out []*OpsUpstreamErrorEvent
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	for _, ev := range out {
		normalizeOpsUpstreamProxyAttribution(ev)
	}
	return out, nil
}

// normalizeOpsUpstreamErrorsJSON materializes missing proxy attribution on
// stored JSON for detail reads. It edits only the attribution keys of events
// that lack them, so keys written by older struct versions and the original
// key order survive; nothing is re-marshaled through the current struct.
func normalizeOpsUpstreamErrorsJSON(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw, nil
	}
	if !gjson.Valid(raw) {
		return "", errors.New("upstream_errors is not valid JSON")
	}
	parsed := gjson.Parse(raw)
	if !parsed.IsArray() {
		return "", errors.New("upstream_errors is not a JSON array")
	}
	out := raw
	for i, ev := range parsed.Array() {
		if !ev.IsObject() {
			continue
		}
		prefix := strconv.Itoa(i) + "."
		proxyID := ev.Get("proxy_id")
		proxyName := strings.TrimSpace(ev.Get("proxy_name").String())
		hasValidID := proxyID.Exists() && proxyID.Type == gjson.Number && proxyID.Int() > 0
		var err error
		switch {
		case hasValidID:
			if proxyName == "" {
				out, err = sjson.Set(out, prefix+"proxy_name", opsProxyNameUnnamed)
			}
		case proxyName == opsProxyNameDirect:
			if !proxyID.Exists() || proxyID.Type != gjson.Null {
				out, err = sjson.Set(out, prefix+"proxy_id", nil)
			}
		default:
			if out, err = sjson.Set(out, prefix+"proxy_id", nil); err == nil {
				out, err = sjson.Set(out, prefix+"proxy_name", opsProxyNameUnknown)
			}
		}
		if err != nil {
			return "", err
		}
	}
	return out, nil
}

// safeUpstreamURL returns scheme + host + path from a URL, stripping query/fragment
// to avoid leaking sensitive query parameters (e.g. OAuth tokens).
func safeUpstreamURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if idx := strings.IndexByte(rawURL, '?'); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	if idx := strings.IndexByte(rawURL, '#'); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	return rawURL
}

// MarkOpsClientCancellation records the request outcome without writing to a disconnected peer.
const OpsClientCanceledCode = "client_canceled"

func MarkOpsClientCancellation(c *gin.Context, stream bool) {
	MarkOpsStreamErrorValue(c, OpsStreamError{ErrType: OpsClientCanceledCode, Code: OpsClientCanceledCode, Message: "Client disconnected: context canceled", IntendedStatus: 499, RequestScoped: true, NonStream: !stream})
}
