//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

// 一条用例覆盖一种 kind：给出分类器实际会遇到的错误，断言它落到的 kind 及其
// 策略。表里没有对应用例的 policy、或用例期望的 kind 没有 policy，都算失败，
// 这样新增 kind 时不会漏配，也不会有没人断言的策略。
func TestBodyReadErrorPolicyCoversEveryKind(t *testing.T) {
	cases := []struct {
		kind          string
		err           error
		wantStatus    int
		wantErrorType string
		wantRecord    bool
	}{
		{bodyReadKindMaxBytes, &http.MaxBytesError{Limit: 1024}, http.StatusRequestEntityTooLarge, "invalid_request_error", true},
		{bodyReadKindClientDisconnect, context.Canceled, statusClientClosedRequest, "invalid_request_error", false},
		{bodyReadKindTruncatedBody, io.ErrUnexpectedEOF, http.StatusBadRequest, "invalid_request_error", true},
		{bodyReadKindTransportTimeout, os.ErrDeadlineExceeded, http.StatusRequestTimeout, "api_error", true},
		{bodyReadKindTransport, &net.OpError{Op: "read", Err: syscall.ECONNABORTED}, http.StatusBadRequest, "invalid_request_error", true},
		{bodyReadKindUnsupportedContentEncoding, errors.New("decode content-encoding: unsupported content-encoding \"br\""), http.StatusUnsupportedMediaType, "invalid_request_error", true},
		{bodyReadKindDecodeContentEncoding, errors.New("decode content-encoding: gzip: invalid header"), http.StatusBadRequest, "invalid_request_error", true},
		// io_read 是分类器自己的兜底，保持这张表出现之前的答案。
		{bodyReadKindIORead, errors.New("boom"), http.StatusBadRequest, "invalid_request_error", true},
	}

	seenMessages := make(map[string]string, len(cases))
	asserted := make(map[string]struct{}, len(cases))
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			require.Equal(t, tc.kind, requestBodyReadErrorKind(context.Background(), tc.err))
			policy, ok := bodyReadErrorPolicies[tc.kind]
			require.Truef(t, ok, "kind %q has no explicit policy", tc.kind)
			require.Equal(t, tc.wantStatus, policy.Status)
			require.Equal(t, tc.wantErrorType, policy.ErrorType)
			require.Equal(t, tc.wantRecord, policy.Record)
			require.NotEmpty(t, policy.Message)

			// ops_error_logs 存的是 message 而不是 kind，两种 kind 共用文案就
			// 等于在下游把它们又合回去了。
			if prev, dup := seenMessages[policy.Message]; dup {
				t.Fatalf("kind %q reuses the message of %q: %q", tc.kind, prev, policy.Message)
			}
			seenMessages[policy.Message] = tc.kind
		})
		asserted[tc.kind] = struct{}{}
	}

	for kind := range bodyReadErrorPolicies {
		require.Containsf(t, asserted, kind, "policy %q has no expectation in this table", kind)
	}
}

// 未识别的 kind 必须退化为原有行为，而不是零值策略（那会写出 HTTP 0）。
// "原有行为"包括 error type：表出现之前每个入口都答 invalid_request_error，
// 换成 api_error 会让它在 ops_error_logs 里变成 phase=internal/P2。
func TestBodyReadErrorPolicyUnknownKindFallsBack(t *testing.T) {
	policy := bodyReadErrorPolicyFor("some_future_kind")
	require.Equal(t, bodyReadErrorFallbackPolicy, policy)
	require.Equal(t, http.StatusBadRequest, policy.Status)
	require.Equal(t, "invalid_request_error", policy.ErrorType)
	require.Equal(t, "Failed to read request body", policy.Message)
	require.True(t, policy.Record)
	require.Equal(t, bodyReadErrorFallbackPolicy, bodyReadErrorPolicyFor(bodyReadKindIORead))
}

// 只有 isKnownOpsErrorType 接受的类型能活过 normalizeOpsErrorType；其余一律
// 被改写成 "api_error"，区分就没了。
func TestBodyReadErrorPolicyUsesKnownOpsErrorTypes(t *testing.T) {
	for kind, p := range bodyReadErrorPolicies {
		require.Truef(t, isKnownOpsErrorType(p.ErrorType),
			"kind %q: error type %q is not in isKnownOpsErrorType and would be normalized away", kind, p.ErrorType)
	}
	require.True(t, isKnownOpsErrorType(bodyReadErrorFallbackPolicy.ErrorType))
}

// 非超时的网络错误不能再被报成 "Timed out"。
func TestRequestBodyReadErrorKind_SplitsTransportTimeout(t *testing.T) {
	require.Equal(t, bodyReadKindTransportTimeout, requestBodyReadErrorKind(context.Background(), os.ErrDeadlineExceeded))
	require.Equal(t, bodyReadKindTransportTimeout, requestBodyReadErrorKind(context.Background(), &net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}))
	require.Equal(t, bodyReadKindTransport, requestBodyReadErrorKind(context.Background(), &net.OpError{Op: "read", Err: syscall.ECONNABORTED}))
	require.Equal(t, bodyReadKindTransport, requestBodyReadErrorKind(context.Background(), &net.OpError{Op: "read", Err: syscall.EHOSTUNREACH}))
	// 断连优先于 transport：ECONNRESET 也是 net.OpError。
	require.Equal(t, bodyReadKindClientDisconnect, requestBodyReadErrorKind(context.Background(), &net.OpError{Op: "read", Err: syscall.ECONNRESET}))
	require.Equal(t, bodyReadKindTruncatedBody, requestBodyReadErrorKind(context.Background(), http2.StreamError{Code: http2.ErrCodeInternal}))
}

// 走一遍 helper：状态码来自策略表，ops 跳过标记只对 Record=false 的 kind 置位，
// error_kind 只算一次并原样进日志。
func TestRespondRequestBodyReadFailureMapsErrorsEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name        string
		err         error
		wantKind    string
		wantStatus  int
		wantSkipLog bool
	}{
		{"客户端断开", context.Canceled, bodyReadKindClientDisconnect, statusClientClosedRequest, true},
		{"连接被重置", syscall.ECONNRESET, bodyReadKindClientDisconnect, statusClientClosedRequest, true},
		{"h2 客户端取消", http2.StreamError{Code: http2.ErrCodeCancel}, bodyReadKindClientDisconnect, statusClientClosedRequest, true},
		{"h2 连接断开", errors.New(h2ClientDisconnectedMessage), bodyReadKindClientDisconnect, statusClientClosedRequest, true},
		{"h2 连接断开被包装", fmt.Errorf("read body: %w", errors.New(h2ClientDisconnectedMessage)), bodyReadKindClientDisconnect, statusClientClosedRequest, true},
		{"传输被截断", io.ErrUnexpectedEOF, bodyReadKindTruncatedBody, http.StatusBadRequest, false},
		{"h2 内部错误", http2.StreamError{Code: http2.ErrCodeInternal}, bodyReadKindTruncatedBody, http.StatusBadRequest, false},
		{"读超时", os.ErrDeadlineExceeded, bodyReadKindTransportTimeout, http.StatusRequestTimeout, false},
		{"网络错误非超时", &net.OpError{Op: "read", Err: syscall.ECONNABORTED}, bodyReadKindTransport, http.StatusBadRequest, false},
		{"不支持的编码", errors.New(`decode Content-Encoding "br": unsupported Content-Encoding`), bodyReadKindUnsupportedContentEncoding, http.StatusUnsupportedMediaType, false},
		{"请求体过大", &http.MaxBytesError{Limit: 1024}, bodyReadKindMaxBytes, http.StatusRequestEntityTooLarge, false},
		{"未知错误兜底", errors.New("boom"), bodyReadKindIORead, http.StatusBadRequest, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)
			log, logs := newObservedLogger(t)

			var gotStatus int
			var gotType, gotMessage string
			RespondRequestBodyReadFailure(c, log, tc.err,
				func(_ *gin.Context, status int, errType, message string) {
					gotStatus = status
					gotType = errType
					gotMessage = message
				})

			policy := bodyReadErrorPolicyFor(tc.wantKind)
			require.Equal(t, tc.wantStatus, gotStatus)
			require.Equal(t, policy.ErrorType, gotType)
			require.NotEmpty(t, gotMessage)
			require.Equal(t, tc.wantSkipLog, shouldSkipOpsErrorRecord(c), "ops_error_logs skip flag mismatch")

			entries := logs.All()
			require.Len(t, entries, 1)
			require.Equal(t, tc.wantKind, entries[0].ContextMap()["error_kind"])
		})
	}
}

// 带上限值的 413 文案早于策略表存在，不能被静态兜底文案顶掉，否则运维从响应
// 里看不到配置的上限。
func TestRespondRequestBodyReadFailureKeepsMaxBytesLimitInMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)

	var gotStatus int
	var gotType, gotMessage string
	RespondRequestBodyReadFailure(c, nil, &http.MaxBytesError{Limit: 4096},
		func(_ *gin.Context, status int, errType, message string) {
			gotStatus, gotType, gotMessage = status, errType, message
		})

	require.Equal(t, http.StatusRequestEntityTooLarge, gotStatus)
	require.Equal(t, "invalid_request_error", gotType)
	require.Equal(t, buildBodyTooLargeMessage(4096), gotMessage)
	require.NotEqual(t, bodyReadErrorPolicies[bodyReadKindMaxBytes].Message, gotMessage)
	require.False(t, shouldSkipOpsErrorRecord(c))
}

// nil logger 与 nil context 都不能 panic：部分入口没有请求级 logger。
func TestRespondRequestBodyReadFailureToleratesNilLoggerAndContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)

	rendered := false
	require.NotPanics(t, func() {
		RespondRequestBodyReadFailure(c, nil, context.Canceled, func(*gin.Context, int, string, string) { rendered = true })
	})
	require.True(t, rendered)

	require.NotPanics(t, func() { markOpsSkipErrorRecord(nil) })
	require.False(t, shouldSkipOpsErrorRecord(nil))
}

// 入口不允许再各自内联通用 400：兜底文案只能出现在策略表里，新增入口必须走
// RespondRequestBodyReadFailure，否则 h2 断连又会以 invalid_request_error 进
// ops_error_logs。
func TestBodyReadFallbackMessageOnlyLivesInPolicyTable(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "request_body_read_log.go" {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		require.NotContainsf(t, string(src), bodyReadErrorFallbackPolicy.Message,
			"%s answers a body read failure inline; route it through RespondRequestBodyReadFailure", name)
	}
}
