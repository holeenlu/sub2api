//go:build unit

package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// poolCooldownDiagnoser reports a pool whose every model-capable account is
// cooling down until resetAt.
func poolCooldownDiagnoser(resetAt time.Time) *fakeDiagnoser {
	return &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{
		HasAccountsInPool:          true,
		HasModelSupport:            true,
		AllModelCapableRateLimited: true,
		EarliestRateLimitResetAt:   &resetAt,
	}}
}

// selectionFailure mirrors what the scheduler returns when a pool comes up
// empty: ErrNoAvailableAccounts wrapped with its compact summary.
func selectionFailure(model string, summary string) error {
	return fmt.Errorf("%w supporting model: %s (%s)", service.ErrNoAvailableAccounts, model, summary)
}

// runNoAccountRoute drives a real gin engine through the same sequence the
// Anthropic /v1/messages call site uses on the no-account path, so the test
// observes the wire-level response rather than the classification struct.
func runNoAccountRoute(t *testing.T, diag service.ModelAvailabilityDiagnoser, selectionErr error) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	var captured *gin.Context
	h := &GatewayHandler{}
	engine.POST("/v1/messages", func(c *gin.Context) {
		captured = c
		apiKey := &service.APIKey{GroupID: ptrInt64(7)}
		cls := classifyNoAccountErrorFromGin(c, diag, apiKey, "claude-opus-4-8", "claude-opus-4-8", service.PlatformAnthropic)
		if !cls.ModelNotFound {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, selectionErr)
		}
		h.errorResponse(c, cls.Status, cls.ErrType, cls.messageWithSelectionDetail("No available accounts: ", selectionErr))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-8"}`))
	engine.ServeHTTP(w, req)
	return w, captured
}

func decodeErrorBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var parsed struct {
		Error map[string]any `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &parsed), "body must be JSON: %s", body)
	require.NotNil(t, parsed.Error)
	return parsed.Error
}

func TestNoAccountRoute_PoolCooldown_Emits429WithRetryAfter(t *testing.T) {
	selectionErr := selectionFailure("claude-opus-4-8", "total=3 eligible=0 unschedulable=3")
	w, c := runNoAccountRoute(t, poolCooldownDiagnoser(time.Now().Add(90*time.Second)), selectionErr)

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Equal(t, "90", w.Result().Header.Get("Retry-After"), "remaining 89.9xs must round up to 90")

	errObj := decodeErrorBody(t, w.Body.Bytes())
	require.Equal(t, "rate_limit_error", errObj["type"])
	require.Equal(t, noAccountRateLimitedMessage, errObj["message"],
		"the 429 message is a contract of its own and must not be replaced by the scheduler summary")

	// ops 归因：仍是 routing 容量问题，不计入 SLA，也不是本地模型配置错误。
	require.True(t, isOpsRoutingCapacityLimited(c))
	require.False(t, service.HasOpsClientBusinessLimited(c))
	phase, businessLimited, _, _ := classifyOpsErrorLog(c, "rate_limit_error", noAccountRateLimitedMessage, "", http.StatusTooManyRequests)
	require.Equal(t, "routing", phase)
	require.True(t, businessLimited)
}

func TestNoAccountRoute_OtherExhaustion_Stays503WithSelectionDetail(t *testing.T) {
	selectionErr := selectionFailure("claude-opus-4-8", "total=2 eligible=0 unschedulable=2")
	diag := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	w, _ := runNoAccountRoute(t, diag, selectionErr)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Empty(t, w.Result().Header.Get("Retry-After"))

	errObj := decodeErrorBody(t, w.Body.Bytes())
	require.Equal(t, "api_error", errObj["type"])
	require.Equal(t, "No available accounts: "+selectionErr.Error(), errObj["message"],
		"the plain 503 keeps surfacing the scheduler summary for operators")
}

func TestNoAccountRoute_ModelNotFound_KeepsItsMessageAndNoRetryAfter(t *testing.T) {
	selectionErr := selectionFailure("claude-opus-4-8", "total=2 eligible=0 model_unsupported=2")
	diag := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}}
	w, _ := runNoAccountRoute(t, diag, selectionErr)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Empty(t, w.Result().Header.Get("Retry-After"))
	errObj := decodeErrorBody(t, w.Body.Bytes())
	require.Equal(t, "model_not_found", errObj["type"])
	require.Contains(t, errObj["message"], "claude-opus-4-8")
}

// 流已经开始（响应头已提交）时，429 只能以 SSE 错误事件送达：事件必须是
// rate_limit_error，而 Retry-After 头已无法再写入。
func TestNoAccountRoute_StreamAlreadyStarted_SSECarriesRateLimitError(t *testing.T) {
	c, w := newGinContextForEndpoint(t, "/v1/chat/completions")
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	apiKey := &service.APIKey{GroupID: ptrInt64(7)}
	cls := classifyNoAccountErrorFromGin(c, poolCooldownDiagnoser(time.Now().Add(time.Minute)), apiKey, "gpt-5", "gpt-5", service.PlatformOpenAI)
	require.Equal(t, http.StatusTooManyRequests, cls.Status)

	h := &OpenAIGatewayHandler{}
	h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, true)

	body := w.Body.String()
	require.Contains(t, body, `"type":"rate_limit_error"`)
	require.Contains(t, body, noAccountRateLimitedMessage)
	require.Equal(t, http.StatusOK, w.Result().StatusCode, "committed status cannot change")
	require.Empty(t, w.Result().Header.Get("Retry-After"), "headers were already flushed")
}

func TestMessageWithSelectionDetail(t *testing.T) {
	err := errors.New("total=1 eligible=0")
	plain := noAccountErrorClassification{Status: http.StatusServiceUnavailable, Message: "Service temporarily unavailable"}
	require.Equal(t, "No available accounts: total=1 eligible=0", plain.messageWithSelectionDetail("No available accounts: ", err))
	require.Equal(t, "Service temporarily unavailable", plain.messageWithSelectionDetail("No available accounts: ", nil))

	limited := noAccountErrorClassification{Status: http.StatusTooManyRequests, Message: noAccountRateLimitedMessage, RetryAfter: time.Second}
	require.Equal(t, noAccountRateLimitedMessage, limited.messageWithSelectionDetail("No available accounts: ", err))

	notFound := noAccountErrorClassification{Status: http.StatusNotFound, Message: "Model x is not supported", ModelNotFound: true}
	require.Equal(t, "Model x is not supported", notFound.messageWithSelectionDetail("No available accounts: ", err))
}

func TestSetNoAccountRetryAfterHeader(t *testing.T) {
	c := newTestGinContextWithRequest()
	setNoAccountRetryAfterHeader(c, noAccountErrorClassification{Status: http.StatusServiceUnavailable, RetryAfter: 30 * time.Second})
	require.Empty(t, c.Writer.Header().Get("Retry-After"), "only 429 carries the header")

	setNoAccountRetryAfterHeader(c, noAccountErrorClassification{Status: http.StatusTooManyRequests})
	require.Empty(t, c.Writer.Header().Get("Retry-After"), "a 429 without a hint writes nothing")

	setNoAccountRetryAfterHeader(c, noAccountErrorClassification{Status: http.StatusTooManyRequests, RetryAfter: 30 * time.Second})
	require.Equal(t, "30", c.Writer.Header().Get("Retry-After"))

	setNoAccountRetryAfterHeader(nil, noAccountErrorClassification{Status: http.StatusTooManyRequests, RetryAfter: time.Second})
}

func TestNoAccountWSCloseReason(t *testing.T) {
	require.Equal(t, "no available account",
		noAccountWSCloseReason(noAccountErrorClassification{Status: http.StatusServiceUnavailable}))
	require.Equal(t, "no available account",
		noAccountWSCloseReason(noAccountErrorClassification{Status: http.StatusNotFound, ModelNotFound: true}))
	require.Equal(t, "no available account: rate limited, retry_after=90",
		noAccountWSCloseReason(noAccountErrorClassification{Status: http.StatusTooManyRequests, RetryAfter: 90 * time.Second}))
	require.Equal(t, "no available account: rate limited",
		noAccountWSCloseReason(noAccountErrorClassification{Status: http.StatusTooManyRequests}))
}
