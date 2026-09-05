//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type fakeDiagnoser struct {
	calls []fakeDiagnoseCall
	resp  service.ModelAvailabilityDiagnosis
}

type fakeDiagnoseCall struct {
	GroupID  *int64
	Model    string
	Platform string
}

func (f *fakeDiagnoser) DiagnoseModelAvailabilityForPlatform(
	_ context.Context,
	groupID *int64,
	model, platform string,
) service.ModelAvailabilityDiagnosis {
	f.calls = append(f.calls, fakeDiagnoseCall{
		GroupID:  groupID,
		Model:    model,
		Platform: platform,
	})
	return f.resp
}

func ptrInt64(v int64) *int64 { return &v }

// newTestGinContextWithRequest wraps the bare newTestGinContext helper
// (defined in openai_gateway_cyber_test.go) by additionally attaching a stub
// *http.Request so the classifier can extract c.Request.Context().
func newTestGinContextWithRequest() *gin.Context {
	c := newTestGinContext()
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	return c
}

func TestClassifyNoAccountError_NilDiagnoser_Falls503(t *testing.T) {
	c := newTestGinContextWithRequest()
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, nil, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound)
}

func TestClassifySelectionFailureError_RateLimitedPool(t *testing.T) {
	fallback := noAccountErrorClassification{
		Status:             http.StatusServiceUnavailable,
		ErrType:            "api_error",
		Message:            "Service temporarily unavailable",
		cooldownRetryAfter: 30 * time.Second,
	}

	got := classifySelectionFailureError(
		fmt.Errorf("no available accounts supporting model: gpt-5.6-sol (total=3 eligible=0 model_rate_limited=3)"),
		fallback,
	)

	require.Equal(t, http.StatusTooManyRequests, got.Status)
	require.Equal(t, "rate_limit_error", got.ErrType)
	require.Contains(t, got.Message, "rate-limited")
	require.Equal(t, 30*time.Second, got.RetryAfter)
	require.Equal(t, fallback, classifySelectionFailureError(fmt.Errorf("model_rate_limited=0"), fallback))
	require.Equal(t, fallback, classifySelectionFailureError(fmt.Errorf("no available accounts"), fallback))
}

func TestClassifyNoAccountError_AllModelCapableRateLimited_Returns429(t *testing.T) {
	c := newTestGinContextWithRequest()
	reset := time.Now().Add(90 * time.Second)
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            true,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &reset,
	}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusTooManyRequests, cls.Status)
	require.Equal(t, "rate_limit_error", cls.ErrType)
	require.False(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, "rate-limited")
	require.InDelta(t, 90, cls.RetryAfter.Seconds(), 1)
	require.False(t, service.HasOpsClientBusinessLimited(c), "a pool cooldown is routing capacity, not a local model-configuration problem")
}

func TestClassifyNoAccountError_FableWindowIncludesClientDetails(t *testing.T) {
	c := newTestGinContextWithRequest()
	reset := time.Now().Add(2*time.Hour + 3*time.Minute + 4*time.Second)
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            true,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &reset,
		RateLimit: &service.RateLimitAttribution{
			Scope:   "model",
			Window:  "7d_oi",
			Reason:  service.AnthropicFableWindowExhaustedReason,
			Model:   "claude-fable-5-1",
			ResetAt: &reset,
		},
	}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "claude-fable-5-1", "claude-fable-5-1", service.PlatformAnthropic)

	require.Equal(t, http.StatusTooManyRequests, cls.Status)
	require.Equal(t, "rate_limit_error", cls.ErrType)
	require.Equal(t, "anthropic_fable_7d_oi_exhausted", cls.ErrorCode)
	require.Equal(t, "model", cls.LimitScope)
	require.Equal(t, "7d_oi", cls.LimitWindow)
	require.Contains(t, cls.Message, "Fable 5.1")
	require.Contains(t, cls.Message, "7-day window resets in")
	require.Greater(t, cls.RetryAfterSeconds, int64(7000))
	require.Less(t, cls.RetryAfterSeconds, int64(7500))

	recorder := httptest.NewRecorder()
	renderContext, _ := gin.CreateTestContext(recorder)
	renderContext.Set(noAccountClassificationContextKey, cls)
	(&GatewayHandler{}).errorResponse(renderContext, cls.Status, cls.ErrType, cls.Message)
	require.Contains(t, recorder.Body.String(), `"code":"anthropic_fable_7d_oi_exhausted"`)
	require.Contains(t, recorder.Body.String(), `"limit_window":"7d_oi"`)
}

// 404 优先级高于 429：模型压根没配，就不该让客户端去重试。
func TestClassifyNoAccountError_ModelNotFoundWinsOver429(t *testing.T) {
	c := newTestGinContextWithRequest()
	reset := time.Now().Add(90 * time.Second)
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            false,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &reset,
	}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.True(t, cls.ModelNotFound)
	require.Zero(t, cls.RetryAfter)
}

// 只有部分账号冷却时保持 503：诊断层不置位，分类器就不该自作主张，
// 哪怕诊断顺带给出了最早恢复时刻。
func TestClassifyNoAccountError_PartialCooldownStays503(t *testing.T) {
	c := newTestGinContextWithRequest()
	reset := time.Now().Add(90 * time.Second)
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:        true,
		HasModelSupport:          true,
		EarliestRateLimitResetAt: &reset,
	}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.Zero(t, cls.RetryAfter)
}

func TestRetryAfterFromReset(t *testing.T) {
	require.Equal(t, poolCooldownRetryAfterMin, retryAfterFromReset(nil), "nil reset falls back to the floor")

	past := time.Now().Add(-time.Hour)
	require.Equal(t, poolCooldownRetryAfterMin, retryAfterFromReset(&past), "a reset in the past must not yield zero")

	far := time.Now().Add(3 * time.Hour)
	require.Equal(t, poolCooldownRetryAfterMax, retryAfterFromReset(&far), "far-future resets are capped")

	// 89.4s 剩余必须给 90 而不是 89：按头重试的客户端提前 0.4s 回来只会再吃一次 429。
	fractional := time.Now().Add(89*time.Second + 400*time.Millisecond)
	require.Equal(t, 90*time.Second, retryAfterFromReset(&fractional))

	// 不足 1 秒的剩余时间同样向上取整到 1，不能发出 0 触发重试风暴。
	subSecond := time.Now().Add(200 * time.Millisecond)
	require.Equal(t, time.Second, retryAfterFromReset(&subSecond))

	whole := time.Now().Add(42 * time.Second)
	got := retryAfterFromReset(&whole)
	require.Equal(t, time.Duration(0), got%time.Second, "Retry-After is expressed in whole seconds")
	require.InDelta(t, 42, got.Seconds(), 1)
}

func TestClassifyNoAccountError_NilAPIKey_Falls503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}

	cls := classifyNoAccountErrorFromGin(c, fd, nil, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
	require.Empty(t, fd.calls, "diagnoser must not be consulted when apiKey missing")
}

func TestClassifyNoAccountError_NilGroupID_Falls503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: nil}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
	require.Empty(t, fd.calls, "diagnoser must not be consulted when group not bound")
}

func TestClassifyNoAccountError_EmptyModel_Falls503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "   ", "", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
	require.Empty(t, fd.calls)
}

func TestClassifyNoAccountError_ModelNotSupported_Returns404(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(42)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5.1-codex-mini", "gpt-5.1-codex-mini", service.PlatformOpenAI)

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.Equal(t, "model_not_found", cls.ErrType)
	require.True(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, "gpt-5.1-codex-mini", "message must surface the requested model")

	require.Len(t, fd.calls, 1)
	require.Equal(t, "gpt-5.1-codex-mini", fd.calls[0].Model)
	require.Equal(t, service.PlatformOpenAI, fd.calls[0].Platform)
	require.NotNil(t, fd.calls[0].GroupID)
	require.Equal(t, int64(42), *fd.calls[0].GroupID)
	require.True(t, service.HasOpsClientBusinessLimited(c))
	require.Equal(t, service.OpsClientBusinessLimitedReasonLocalModelConfiguration, service.OpsClientBusinessLimitedReason(c))
}

func TestClassifyOpenAICompatibleNoAccountError_GrokUsesGrokPlatform(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	groupID := int64(43)
	apiKey := &service.APIKey{
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformGrok,
		},
	}

	cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, fd, apiKey, "grok-4.5", "grok-4.5")

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.Equal(t, "model_not_found", cls.ErrType)
	require.True(t, cls.ModelNotFound)
	require.Len(t, fd.calls, 1)
	require.Equal(t, service.PlatformGrok, fd.calls[0].Platform)
	require.True(t, service.HasOpsClientBusinessLimited(c))
	require.Equal(t, service.OpsClientBusinessLimitedReasonLocalModelConfiguration, service.OpsClientBusinessLimitedReason(c))

	logErr := openAICompatibleSelectionErrorForLog(
		fmt.Errorf("no available OpenAI accounts supporting model: grok-4.5"),
		service.PlatformGrok,
	)
	require.EqualError(t, logErr, "no available Grok accounts supporting model: grok-4.5")
}

func TestClassifyNoAccountError_PureClassifierDoesNotMarkGinContext(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountError(c.Request.Context(), fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.True(t, cls.ModelNotFound)
	require.False(t, service.HasOpsClientBusinessLimited(c))
	require.Empty(t, service.OpsClientBusinessLimitedReason(c))
}

func TestClassifyNoAccountError_HasModelSupport_KeepsRoutingMessageGenerationToCaller(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status, "model exists somewhere — caller stays on 503")
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound)
}

// Non-rate-limit exhaustion (overload, temporary unschedulability, quota
// pause, runtime block) stays on 503: the diagnoser sees the model-supporting
// account but reports no pool-wide cooldown, so there is no honest
// Retry-After to hand out. The all-cooldown case is covered by
// TestClassifyNoAccountError_AllModelCapableRateLimited_Returns429.
func TestClassifyNoAccountError_ModelSupportedButExhausted_Returns503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "claude-opus-4-8", "claude-opus-4-8", service.PlatformAnthropic)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound, "temporary exhaustion must remain retryable")
}

func TestClassifyNoAccountError_NoAccountsInPool_Stays503(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: false, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status, "empty pool is a service-availability issue, not a model issue")
	require.False(t, cls.ModelNotFound)
}

func TestClassifyNoAccountError_DisplayModelOverridesRoutingForMessage(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "claude-3-fancy", service.PlatformOpenAI)

	require.True(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, "claude-3-fancy", "user-facing message must reference the model the user asked for, not the post-mapping routing model")
	require.Len(t, fd.calls, 1)
	require.Equal(t, "gpt-5", fd.calls[0].Model, "diagnosis must run against the routing model (post group dispatch mapping)")
}

func TestClassifyNoAccountError_FromGin_NilContextStillSafe(t *testing.T) {
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(nil, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusNotFound, cls.Status, "even with a nil gin context the classifier must still run and yield a coherent response")
	require.True(t, cls.ModelNotFound)
	require.False(t, service.HasOpsClientBusinessLimited(nil))
	require.Empty(t, service.OpsClientBusinessLimitedReason(nil))
}

// 权威的 404 model_not_found 不能被"账号被限流"的 429 盖掉。
//
// 选号失败的错误串同时携带多种过滤原因，例如
// "pool=9, filtered: model_not_supported=8 model_rate_limited=1"：8 个账号根本不支持该模型，
// 剩下 1 个恰好处于模型级冷却。此时 classifyNoAccountError 已通过持久化判据确认整个分组
// 没有账号能服务该模型（ModelNotFound=true），改判成 429 "All available accounts are
// currently rate-limited" 是错误诊断——重试永远不会成功，而把 429 当限流的客户端会反复
// 重试并吞掉 body（Codex 只显示 "exceeded retry limit"），恰好丢掉唯一说明真实原因的信息。
func TestClassifySelectionFailureError_ModelNotFoundIsNotOverriddenByRateLimited(t *testing.T) {
	modelNotFound := noAccountErrorClassification{
		Status:        http.StatusNotFound,
		ErrType:       "model_not_found",
		Message:       `Model "gpt-5.3-codex" is not supported by any configured account in this group`,
		ModelNotFound: true,
	}

	got := classifySelectionFailureError(
		fmt.Errorf("no available OpenAI accounts supporting model: gpt-5.3-codex "+
			"(pool=9, filtered: model_not_supported=8 model_rate_limited=1)"),
		modelNotFound,
	)

	require.Equal(t, modelNotFound, got,
		"分组里没有任何账号能服务该模型时，模型级冷却不该把 404 改判成 429")
}

// 真实调用点的顺序：先 classifyNoAccountErrorFromGin，再 classifySelectionFailureError。
// 覆盖这条链路是为了同时锁住 ops 归因——调用点用 ModelNotFound 决定是否标记
// routing capacity limited，一旦 404 被改判成 429，同一个请求会既被标成
// local model configuration 又被标成容量问题，自相矛盾。
func TestClassifySelectionFailureError_CallSiteChainKeepsModelNotFoundAttribution(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(43)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5.3-codex", "gpt-5.3-codex", service.PlatformOpenAI)
	cls = classifySelectionFailureError(
		fmt.Errorf("no available OpenAI accounts supporting model: gpt-5.3-codex "+
			"(pool=9, filtered: model_not_supported=8 model_rate_limited=1)"),
		cls,
	)

	require.Equal(t, http.StatusNotFound, cls.Status)
	require.Equal(t, "model_not_found", cls.ErrType)
	require.True(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, "gpt-5.3-codex")
	require.True(t, service.HasOpsClientBusinessLimited(c))
	require.Equal(t, service.OpsClientBusinessLimitedReasonLocalModelConfiguration, service.OpsClientBusinessLimitedReason(c))
}

// 池子里确实存在能服务该模型、只是全部在冷却的账号时，429 改判仍需保留：
// 这种情况 fallback 是 503（HasModelSupport=true），重试是有意义的。
func TestClassifySelectionFailureError_StillUpgradesNonModelNotFoundFallback(t *testing.T) {
	fallback := noAccountErrorClassification{
		Status:             http.StatusServiceUnavailable,
		ErrType:            "api_error",
		Message:            "Service temporarily unavailable",
		cooldownRetryAfter: 45 * time.Second,
	}

	got := classifySelectionFailureError(
		fmt.Errorf("no available accounts supporting model: gpt-5.6-sol (total=3 eligible=0 model_rate_limited=3)"),
		fallback,
	)

	require.Equal(t, http.StatusTooManyRequests, got.Status)
	require.Equal(t, "rate_limit_error", got.ErrType)
	require.False(t, got.ModelNotFound)
	require.Equal(t, 45*time.Second, got.RetryAfter)
}

// 诊断层已经给出带 Retry-After 的 429 时，正则路径不得重建分类：
// 重建会把按真实恢复时刻算出的 Retry-After 丢掉。
func TestClassifySelectionFailureError_KeepsDiagnosis429RetryAfter(t *testing.T) {
	c := newTestGinContextWithRequest()
	reset := time.Now().Add(90 * time.Second)
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            true,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &reset,
	}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)
	cls = classifySelectionFailureErrorFromGin(c,
		fmt.Errorf("no available OpenAI accounts supporting model: gpt-5 (pool=3, filtered: model_rate_limited=3)"),
		cls,
	)

	require.Equal(t, http.StatusTooManyRequests, cls.Status)
	require.Equal(t, "rate_limit_error", cls.ErrType)
	require.InDelta(t, 90, cls.RetryAfter.Seconds(), 1)
	require.Equal(t, "90", c.Writer.Header().Get("Retry-After"))
}

// 诊断只看到部分账号冷却（503），但调度摘要说有账号处于模型级限流：
// 正则路径仍升 429，且 Retry-After 用诊断记下的最早恢复时刻，不再裸奔。
func TestClassifySelectionFailureError_RegexUpgradeCarriesDiagnosisHint(t *testing.T) {
	c := newTestGinContextWithRequest()
	reset := time.Now().Add(45 * time.Second)
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:        true,
		HasModelSupport:          true,
		EarliestRateLimitResetAt: &reset,
	}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)
	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Empty(t, c.Writer.Header().Get("Retry-After"), "the 503 itself carries no header")

	cls = classifySelectionFailureErrorFromGin(c,
		fmt.Errorf("no available OpenAI accounts supporting model: gpt-5 (pool=3, filtered: model_rate_limited=1 quota_auto_pause_7d=2)"),
		cls,
	)

	require.Equal(t, http.StatusTooManyRequests, cls.Status)
	require.Equal(t, "rate_limit_error", cls.ErrType)
	require.InDelta(t, 45, cls.RetryAfter.Seconds(), 1)
	require.Equal(t, "45", c.Writer.Header().Get("Retry-After"))
}

// 诊断没有任何冷却信息（诊断器缺失或查询失败）时，摘要里的限流计数不足以
// 单独改判：429 的全部价值在于那句 "retry after N seconds"，编造一个不诚实，
// 裸发一个又正是 B3 要消除的形态，所以停在诊断给出的 503。
func TestClassifySelectionFailureError_RegexUpgradeWithoutHintStaysAt503(t *testing.T) {
	c := newTestGinContextWithRequest()
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, nil, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)
	cls = classifySelectionFailureErrorFromGin(c,
		fmt.Errorf("no available accounts supporting model: gpt-5 (total=3 eligible=0 model_rate_limited=3)"),
		cls,
	)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.Zero(t, cls.RetryAfter)
	require.Empty(t, c.Writer.Header().Get("Retry-After"))
}

// 每一个 429 都必须带 Retry-After：正则路径能改判，当且仅当诊断记下了最早
// 恢复时刻。这条把不变式本身钉住，避免以后又加一条无提示的升级分支。
func TestClassifySelectionFailureError_Every429CarriesRetryAfter(t *testing.T) {
	summary := fmt.Errorf("no available accounts supporting model: gpt-5 (total=3 eligible=0 model_rate_limited=3)")
	base := noAccountErrorClassification{
		Status:  http.StatusServiceUnavailable,
		ErrType: "api_error",
		Message: "Service temporarily unavailable",
	}

	withHint := base
	withHint.cooldownRetryAfter = 30 * time.Second
	for _, fallback := range []noAccountErrorClassification{base, withHint} {
		got := classifySelectionFailureError(summary, fallback)
		if got.Status == http.StatusTooManyRequests {
			require.Positive(t, got.RetryAfter, "a 429 without Retry-After is exactly the shape we removed")
		}
	}
}

// 摘要里没有限流计数时，链路终点仍是诊断给出的 503，且没有 Retry-After。
func TestClassifySelectionFailureErrorFromGin_NoMatchKeeps503(t *testing.T) {
	c := newTestGinContextWithRequest()
	reset := time.Now().Add(45 * time.Second)
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:        true,
		HasModelSupport:          true,
		EarliestRateLimitResetAt: &reset,
	}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifyNoAccountErrorFromGin(c, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)
	cls = classifySelectionFailureErrorFromGin(c,
		fmt.Errorf("no available accounts supporting model: gpt-5 (total=3 eligible=0 unschedulable=3)"),
		cls,
	)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Zero(t, cls.RetryAfter)
	require.Empty(t, c.Writer.Header().Get("Retry-After"))
}

// 选号错误不是「池子空了」这一族时，诊断结果一概不许改写它。
//
// ErrClaudeCodeOnly（分组只允许 Claude Code 客户端）在全池冷却期间会命中
// AllModelCapableRateLimited，被改判成带 Retry-After 的 429：客户端据此无限
// 重试，而唯一说明真实原因的那句话被 429 的固定文案顶掉了，冷却结束后请求
// 依然失败。这类错误必须停在 503，并让调用点把原始文案接回去。
func TestClassifySelectionError_NonPoolErrorKeeps503AndDetail(t *testing.T) {
	c := newTestGinContextWithRequest()
	reset := time.Now().Add(90 * time.Second)
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            true,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &reset,
	}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifySelectionErrorFromGin(c, service.ErrClaudeCodeOnly, fd, apiKey,
		"claude-opus-4-8", "claude-opus-4-8", service.PlatformAnthropic)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.False(t, cls.ModelNotFound)
	require.Zero(t, cls.RetryAfter)
	require.Empty(t, c.Writer.Header().Get("Retry-After"))
	require.Empty(t, fd.calls, "a non-capacity failure needs no pool diagnosis")
	require.Contains(t,
		cls.messageWithSelectionDetail("No available accounts: ", service.ErrClaudeCodeOnly),
		"only allows Claude Code clients")
}

// 模型压根没配的分组同样不能把 ErrClaudeCodeOnly 改判成 404：那会让
// markOpsRoutingCapacityLimitedIfNoAvailable 被跳过，归因也跟着错。
func TestClassifySelectionError_NonPoolErrorIsNotRewrittenTo404(t *testing.T) {
	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cls := classifySelectionErrorFromGin(c, fmt.Errorf("resolve group: %w", context.DeadlineExceeded),
		fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
	require.False(t, service.HasOpsClientBusinessLimited(c))
}

// 反面：确实是「无可用账号」族（包括被包装过的）时，分类照常进行，并把
// 调度摘要的改判也一起跑完。
func TestClassifySelectionError_PoolExhaustionStillClassified(t *testing.T) {
	reset := time.Now().Add(90 * time.Second)
	diagnosis := service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            true,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &reset,
	}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}

	cases := []struct {
		name string
		err  error
	}{
		{"裸哨兵", service.ErrNoAvailableAccounts},
		{"带摘要", fmt.Errorf("%w supporting model: gpt-5 (total=3 eligible=0 model_rate_limited=3)", service.ErrNoAvailableAccounts)},
		{"compact 变体", service.ErrNoAvailableCompactAccounts},
		{"选号返回空但无错误", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestGinContextWithRequest()
			fd := &fakeDiagnoser{resp: diagnosis}

			cls := classifySelectionErrorFromGin(c, tc.err, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

			require.Equal(t, http.StatusTooManyRequests, cls.Status)
			require.Equal(t, "rate_limit_error", cls.ErrType)
			require.InDelta(t, 90, cls.RetryAfter.Seconds(), 1)
			require.Equal(t, "90", c.Writer.Header().Get("Retry-After"))
			require.Len(t, fd.calls, 1)
		})
	}
}

// OpenAI 兼容入口与 WebSocket 入口共用同一道闸门。
func TestClassifySelectionError_GateAppliesToEveryEntryPoint(t *testing.T) {
	reset := time.Now().Add(90 * time.Second)
	diagnosis := service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            true,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &reset,
	}
	apiKey := &service.APIKey{GroupID: ptrInt64(7), Group: &service.Group{ID: 7, Platform: service.PlatformGrok}}

	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: diagnosis}
	cls := classifyOpenAICompatibleSelectionErrorFromGin(c, service.ErrClaudeCodeOnly, fd, apiKey, "grok-4.5", "grok-4.5")
	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Empty(t, fd.calls)

	c = newTestGinContextWithRequest()
	fd = &fakeDiagnoser{resp: diagnosis}
	cls = classifyOpenAICompatibleSelectionErrorFromGin(c, service.ErrNoAvailableAccounts, fd, apiKey, "grok-4.5", "grok-4.5")
	require.Equal(t, http.StatusTooManyRequests, cls.Status)
	require.Equal(t, service.PlatformGrok, fd.calls[0].Platform)

	wsCls := classifySelectionError(context.Background(), service.ErrClaudeCodeOnly,
		&fakeDiagnoser{resp: diagnosis}, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)
	require.Equal(t, http.StatusServiceUnavailable, wsCls.Status)
	require.Equal(t, "no available account", noAccountWSCloseReason(wsCls))
}

// 渠道模型禁令（ErrSchedulingPolicyRejected）也包着 ErrNoAvailableAccounts，但调度层
// 不会为它走兜底链，池诊断也无从谈起：模型是被禁止而不是被耗尽，必须保住 503 与
// 自己的文案，不能变成一个邀请客户端不断重试的 429。
func TestClassifySelectionError_PolicyRejectionKeeps503(t *testing.T) {
	reset := time.Now().Add(90 * time.Second)
	diagnosis := service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            true,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &reset,
	}
	apiKey := &service.APIKey{GroupID: ptrInt64(7)}
	policyErr := fmt.Errorf("%w supporting model: gpt-5 (channel pricing restriction): %w",
		service.ErrNoAvailableAccounts, service.ErrSchedulingPolicyRejected)

	c := newTestGinContextWithRequest()
	fd := &fakeDiagnoser{resp: diagnosis}

	cls := classifySelectionErrorFromGin(c, policyErr, fd, apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.Equal(t, "api_error", cls.ErrType)
	require.Empty(t, c.Writer.Header().Get("Retry-After"))
	require.Empty(t, fd.calls, "策略拒绝不做池诊断")
	require.Contains(t, cls.messageWithSelectionDetail("No available accounts: ", policyErr), "channel pricing restriction")
}
