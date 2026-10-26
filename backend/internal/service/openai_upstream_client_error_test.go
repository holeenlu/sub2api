package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// issue #5479 上游返回的原始错误体：Codex Desktop 的 automation_update 工具定义沉进
// 会话历史后，OpenAI 每一轮都确定性地拒收。
const openAIInvalidFunctionParametersBody = `{"error":{` +
	`"message":"Invalid schema for function 'automation_update': schema must be a JSON Schema of 'type: \"object\"', got 'type: \"None\"'.",` +
	`"type":"invalid_request_error",` +
	`"param":"input[8].tools[1].tools[2].parameters",` +
	`"code":"invalid_function_parameters"}}`

func newOpenAIUpstreamErrorTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

func newOpenAIUpstreamErrorResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newOpenAIUpstreamErrorTestAccount() *Account {
	return &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Name: "acct"}
}

type modelNotFoundManagedAccountRepo struct{ AccountRepository }

func TestOpenAICompatibleModelNotFound400FailoverScope(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: &modelNotFoundManagedAccountRepo{}}
	body := []byte(`{"error":{"code":"model_not_found","message":"model not found"}}`)

	for _, tc := range []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "openai api key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, want: true},
		{name: "compatible provider", account: &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey}, want: true},
		{name: "anthropic account", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, want: false},
		{name: "missing account", account: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, svc.shouldFailoverOpenAIUpstreamResponse(
				tc.account, http.StatusBadRequest, "model not found", body,
			))
		})
	}

	require.False(t, svc.shouldFailoverOpenAIUpstreamResponse(
		&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		http.StatusBadRequest,
		"Invalid value for temperature",
		[]byte(`{"error":{"code":"invalid_request_error","message":"Invalid value for temperature"}}`),
	))
}

func TestOpenAICompatibleModelNotFound400WithoutManagedCandidatesRemainsTerminal(t *testing.T) {
	svc := &OpenAIGatewayService{}
	body := []byte(`{"error":{"code":"model_not_found","message":"model not found"}}`)

	require.False(t, svc.shouldFailoverOpenAIUpstreamResponse(
		&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		http.StatusBadRequest,
		"model not found",
		body,
	))
}

func TestFailoverOpenAIUpstreamHTTPError_ModelNotFoundIsNextAccountEligible(t *testing.T) {
	c, _ := newOpenAIUpstreamErrorTestContext(t)
	svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: &modelNotFoundManagedAccountRepo{}}
	account := &Account{ID: 42, Name: "compatible", Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	body := []byte(`{"error":{"code":"model_not_found","message":"model not found"}}`)
	resp := newOpenAIUpstreamErrorResponse(http.StatusBadRequest, string(body))

	failoverErr := svc.failoverOpenAIUpstreamHTTPError(
		context.Background(), c, account, resp, body, "model not found", "missing-model",
	)

	require.NotNil(t, failoverErr)
	require.Equal(t, http.StatusBadRequest, failoverErr.StatusCode)
	require.Equal(t, body, failoverErr.ResponseBody)
	require.True(t, failoverErr.ShouldRetryNextAccount())
	require.False(t, IsResponseCommitted(c), "failover must remain eligible before writing downstream")
}

// 主复现：原生 Responses 路径必须回真实的 400 与上游诊断信息，而不是可重试的 502。
//
// 归一成 502 时下游网关（CCH 等）会把确定性的 Schema 错误当成临时上游故障重试，
// issue #5479 实测 30 个失败请求被放大成 60 次上游调用。
func TestHandleErrorResponse_Deterministic400IsNotRewrappedAs502(t *testing.T) {
	c, rec := newOpenAIUpstreamErrorTestContext(t)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	_, err := svc.handleErrorResponse(
		context.Background(),
		newOpenAIUpstreamErrorResponse(http.StatusBadRequest, openAIInvalidFunctionParametersBody),
		c, newOpenAIUpstreamErrorTestAccount(), nil,
	)

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code, "确定性 400 不得被包成可重试的 502")

	body := rec.Body.String()
	require.Equal(t, "invalid_request_error", gjson.Get(body, "error.type").String())
	require.Equal(t, "invalid_function_parameters", gjson.Get(body, "error.code").String())
	require.Equal(t, "input[8].tools[1].tools[2].parameters", gjson.Get(body, "error.param").String(),
		"param 是客户端定位哪个字段非法的唯一线索")
	require.Contains(t, gjson.Get(body, "error.message").String(), "Invalid schema for function 'automation_update'")
	require.NotContains(t, body, "Upstream request failed")

	// 确定性请求错误不该换号重试——换任何账号都是同样的结果。
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "400 不得触发 failover")
}

// 不变式：同一份上游错误体，原生 Responses 与 ChatCompletions/Anthropic 兼容路径
// 必须给出同样的状态码和同样的 message。这两条路径在同一个 service 上，之前一条对
// 一条错，正是本次修复的根因；锁死对称性避免将来只改一边。
func TestHandleErrorResponse_MatchesCompatSiblingForDeterministic400(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	nativeCtx, nativeRec := newOpenAIUpstreamErrorTestContext(t)
	_, nativeErr := svc.handleErrorResponse(
		context.Background(),
		newOpenAIUpstreamErrorResponse(http.StatusBadRequest, openAIInvalidFunctionParametersBody),
		nativeCtx, newOpenAIUpstreamErrorTestAccount(), nil,
	)
	require.Error(t, nativeErr)

	compatCtx, _ := newOpenAIUpstreamErrorTestContext(t)
	var compatStatus int
	var compatType, compatMsg string
	writeError := func(_ *gin.Context, statusCode int, errType, message string) {
		compatStatus, compatType, compatMsg = statusCode, errType, message
	}
	_, compatErr := svc.handleCompatErrorResponse(
		newOpenAIUpstreamErrorResponse(http.StatusBadRequest, openAIInvalidFunctionParametersBody),
		compatCtx, newOpenAIUpstreamErrorTestAccount(), writeError,
	)
	require.Error(t, compatErr)

	require.Equal(t, compatStatus, nativeRec.Code, "两条路径的状态码必须一致")
	require.Equal(t, compatType, gjson.Get(nativeRec.Body.String(), "error.type").String(),
		"两条路径的 error.type 必须一致")
	require.Equal(t, compatMsg, gjson.Get(nativeRec.Body.String(), "error.message").String(),
		"两条路径的 message 必须一致")
}

// 上游只给 message、没有 type/code/param 时，仍要回 400 + 真实 message，
// 缺失字段用 OpenAI 惯例兜底，不得凭空编造 code/param。
func TestHandleErrorResponse_Deterministic400WithoutUpstreamMetadata(t *testing.T) {
	c, rec := newOpenAIUpstreamErrorTestContext(t)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	_, err := svc.handleErrorResponse(
		context.Background(),
		newOpenAIUpstreamErrorResponse(http.StatusBadRequest, `{"error":{"message":"Invalid 'input': expected an array."}}`),
		c, newOpenAIUpstreamErrorTestAccount(), nil,
	)

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := rec.Body.String()
	require.Equal(t, "invalid_request_error", gjson.Get(body, "error.type").String())
	require.Equal(t, "Invalid 'input': expected an array.", gjson.Get(body, "error.message").String())
	require.False(t, gjson.Get(body, "error.code").Exists(), "上游没给 code 就不要编一个")
	require.False(t, gjson.Get(body, "error.param").Exists(), "上游没给 param 就不要编一个")
}

// 上游回非 JSON（反代的 HTML 错误页等）时不得 panic，也不得回空 message。
func TestHandleErrorResponse_Deterministic400WithNonJSONBody(t *testing.T) {
	c, rec := newOpenAIUpstreamErrorTestContext(t)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	_, err := svc.handleErrorResponse(
		context.Background(),
		newOpenAIUpstreamErrorResponse(http.StatusBadRequest, `<html><body>400 Bad Request</body></html>`),
		c, newOpenAIUpstreamErrorTestAccount(), nil,
	)

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := rec.Body.String()
	require.Equal(t, "invalid_request_error", gjson.Get(body, "error.type").String())
	require.NotEmpty(t, gjson.Get(body, "error.message").String())
}

// 作用域守卫：本次只放行 400。其余落到 default 的状态码必须维持原样，
// 避免后续有人顺手把 404/422/5xx 一起改掉。
func TestHandleErrorResponse_NonDeterministicStatusesKeepGeneric502(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		wantStatus int
		wantType   string
		wantMsg    string
	}{
		// 404/405 可能是上游 base_url 配错（运营方问题），不当成客户端错误暴露。
		{"not_found", http.StatusNotFound, `{"error":{"message":"Unknown request URL"}}`,
			http.StatusBadGateway, "upstream_error", "Upstream request failed"},
		{"unprocessable", http.StatusUnprocessableEntity, `{"error":{"message":"Invalid schema for field messages"}}`,
			http.StatusBadGateway, "upstream_error", "Upstream request failed"},
		// 401/402/403 是网关运营方的凭据/账单问题，必须继续对客户端屏蔽上游账号状态。
		// 403 的自由文本不能升级成 durable access-state typed failover；只有明确结构化 code 才可以。
		{"unauthorized", http.StatusUnauthorized, `{"error":{"message":"Incorrect API key provided: sk-abc"}}`,
			http.StatusBadGateway, "upstream_error", "Upstream authentication failed, please contact administrator"},
		{"forbidden", http.StatusForbidden, `{"error":{"message":"Your account is deactivated"}}`,
			http.StatusBadGateway, "upstream_error", "Upstream access forbidden, please contact administrator"},
		// 429 保持独立映射。
		{"rate_limited", http.StatusTooManyRequests, `{"error":{"message":"Rate limit reached"}}`,
			http.StatusTooManyRequests, "rate_limit_error", "Upstream rate limit exceeded, please retry later"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newOpenAIUpstreamErrorTestContext(t)
			svc := &OpenAIGatewayService{cfg: &config.Config{}}

			_, err := svc.handleErrorResponse(
				context.Background(),
				newOpenAIUpstreamErrorResponse(tc.statusCode, tc.body),
				c, newOpenAIUpstreamErrorTestAccount(), nil,
			)
			require.Error(t, err)
			if tc.name == "forbidden" {
				var failoverErr *UpstreamFailoverError
				require.False(t, errors.As(err, &failoverErr))
			}
			require.Equal(t, tc.wantStatus, rec.Code)
			require.Equal(t, tc.wantType, gjson.Get(rec.Body.String(), "error.type").String())
			require.Equal(t, tc.wantMsg, gjson.Get(rec.Body.String(), "error.message").String())
		})
	}
}

// 顺序守卫：管理员配置的错误透传规则在更上游命中，新分支不得抢在它前面。
func TestHandleErrorResponse_PassthroughRuleStillWinsOver400Branch(t *testing.T) {
	c, rec := newOpenAIUpstreamErrorTestContext(t)
	ruleSvc := &ErrorPassthroughService{}
	ruleSvc.setLocalCache([]*model.ErrorPassthroughRule{
		newNonFailoverPassthroughRule(http.StatusBadRequest, "automation_update", http.StatusTeapot, "自定义文案"),
	})
	BindErrorPassthroughService(c, ruleSvc)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	_, err := svc.handleErrorResponse(
		context.Background(),
		newOpenAIUpstreamErrorResponse(http.StatusBadRequest, openAIInvalidFunctionParametersBody),
		c, newOpenAIUpstreamErrorTestAccount(), nil,
	)

	require.Error(t, err)
	require.Equal(t, http.StatusTeapot, rec.Code, "命中透传规则时必须按规则的状态码回写")
	require.Equal(t, "自定义文案", gjson.Get(rec.Body.String(), "error.message").String())
}

func TestIsOpenAIDeterministicClientError(t *testing.T) {
	require.True(t, isOpenAIDeterministicClientError(http.StatusBadRequest))
	for _, status := range []int{
		http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable,
	} {
		require.False(t, isOpenAIDeterministicClientError(status), "status %d", status)
	}
}

func TestWriteOpenAIUpstreamClientError_PayloadShape(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		upstreamMsg string
		wantType    string
		wantCode    string
		wantParam   string
		wantMessage string
	}{
		{
			name:        "full_metadata",
			body:        openAIInvalidFunctionParametersBody,
			upstreamMsg: "Invalid schema for function 'automation_update'",
			wantType:    "invalid_request_error",
			wantCode:    "invalid_function_parameters",
			wantParam:   "input[8].tools[1].tools[2].parameters",
			wantMessage: "Invalid schema for function 'automation_update'",
		},
		{
			name:        "upstream_type_preserved",
			body:        `{"error":{"type":"invalid_prompt","message":"blocked"}}`,
			upstreamMsg: "blocked",
			wantType:    "invalid_prompt",
			wantMessage: "blocked",
		},
		{
			name:        "empty_body_falls_back",
			body:        ``,
			upstreamMsg: "",
			wantType:    "invalid_request_error",
			wantMessage: openAIUpstreamClientErrorFallbackMessage,
		},
		{
			name:        "missing_type_upstream_failure",
			status:      http.StatusBadGateway,
			body:        `{"error":{"code":"server_error"},"request_headers":{"authorization":"secret123"}}`,
			upstreamMsg: "Upstream failed",
			wantType:    "upstream_error",
			wantCode:    "server_error",
			wantMessage: "Upstream failed",
		},
		{
			name:        "missing_type_rate_limit",
			status:      http.StatusTooManyRequests,
			body:        `{"error":{"code":"rate_limit_exceeded"}}`,
			upstreamMsg: "Retry later",
			wantType:    "rate_limit_error",
			wantCode:    "rate_limit_exceeded",
			wantMessage: "Retry later",
		},
		{
			// 调用方传入的 message 已脱敏，必须原样使用，不得回落读取原始 body。
			name:        "sanitized_message_wins_over_raw_body",
			body:        `{"error":{"message":"failed for key=secret123"}}`,
			upstreamMsg: "failed for key=***",
			wantType:    "invalid_request_error",
			wantMessage: "failed for key=***",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newOpenAIUpstreamErrorTestContext(t)

			status := tc.status
			if status == 0 {
				status = http.StatusBadRequest
			}
			writeOpenAIUpstreamClientError(c, status, []byte(tc.body), tc.upstreamMsg)

			require.Equal(t, status, rec.Code)
			body := rec.Body.String()
			require.Equal(t, tc.wantType, gjson.Get(body, "error.type").String())
			require.Equal(t, tc.wantMessage, gjson.Get(body, "error.message").String())
			if tc.wantCode == "" {
				require.False(t, gjson.Get(body, "error.code").Exists())
			} else {
				require.Equal(t, tc.wantCode, gjson.Get(body, "error.code").String())
			}
			if tc.wantParam == "" {
				require.False(t, gjson.Get(body, "error.param").Exists())
			} else {
				require.Equal(t, tc.wantParam, gjson.Get(body, "error.param").String())
			}
			require.NotContains(t, body, "secret123", "原始 body 里的敏感串不得泄漏")
		})
	}
}

// 流式客户端的上下文超限：Codex 只从 response.failed 识别 context_length_exceeded
// 并在下一轮先压缩，HTTP 400 只当普通错误显示。JSON 客户端保持 400。
const openAIContextWindow400Body = `{"error":{"message":"Your input exceeds the context window of this model. Please adjust your input and try again.","type":"invalid_request_error","param":"input","code":"context_length_exceeded"}}`

func requireOpenAIContextWindowResponseFailed(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder) {
	t.Helper()
	require.True(t, IsResponseCommitted(c))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	out := rec.Body.String()
	require.True(t, strings.HasPrefix(out, "event: response.failed\ndata: "), out)
	payload := strings.TrimSuffix(strings.TrimPrefix(out, "event: response.failed\ndata: "), "\n\n")
	require.Equal(t, "response.failed", gjson.Get(payload, "type").String())
	require.Equal(t, "failed", gjson.Get(payload, "response.status").String())
	require.True(t, gjson.Get(payload, "response.created_at").Exists())
	require.Equal(t, "context_length_exceeded", gjson.Get(payload, "response.error.code").String())
	require.Equal(t, "invalid_request_error", gjson.Get(payload, "response.error.type").String())
	require.Equal(t, "input", gjson.Get(payload, "response.error.param").String())
	require.Contains(t, gjson.Get(payload, "response.error.message").String(), "context window")
	streamErr, recorded := GetOpsStreamError(c)
	require.True(t, recorded, "ops keeps the semantic 400 for a failure delivered on a 200 stream")
	require.Equal(t, http.StatusBadRequest, streamErr.IntendedStatus)
	require.Equal(t, "context_length_exceeded", streamErr.ErrType)
}

func TestHandleErrorResponse_ContextWindow400StreamingClientGetsResponseFailed(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
			c, rec := newOpenAIUpstreamErrorTestContext(t)
			markOpenAIResponsesClientStream(c, stream)
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			_, err := svc.handleErrorResponse(context.Background(),
				newOpenAIUpstreamErrorResponse(http.StatusBadRequest, openAIContextWindow400Body),
				c, newOpenAIUpstreamErrorTestAccount(), nil)
			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr), "a full context window never fails over")
			if !stream {
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, "context_length_exceeded", gjson.Get(rec.Body.String(), "error.code").String())
				require.NotContains(t, rec.Body.String(), "response.failed")
				return
			}
			requireOpenAIContextWindowResponseFailed(t, c, rec)
		})
	}
}

func TestHandleErrorResponsePassthrough_ContextWindow400StreamingClientGetsResponseFailed(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
			c, rec := newOpenAIUpstreamErrorTestContext(t)
			markOpenAIResponsesClientStream(c, stream)
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			resp := newOpenAIUpstreamErrorResponse(http.StatusBadRequest, openAIContextWindow400Body)
			err := svc.handleErrorResponsePassthrough(context.Background(), resp, c, newOpenAIUpstreamErrorTestAccount(),
				[]byte(`{"model":"gpt-5.6-sol","stream":true,"input":"hello"}`), []byte(openAIContextWindow400Body))
			require.Error(t, err)
			if !stream {
				// The sanitized passthrough envelope keeps the actionable message only.
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, "upstream_error", gjson.Get(rec.Body.String(), "error.type").String())
				require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "context window")
				require.NotContains(t, rec.Body.String(), "response.failed")
				return
			}
			requireOpenAIContextWindowResponseFailed(t, c, rec)
		})
	}
}
