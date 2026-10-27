//go:build unit

package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIWSHTTPBridgeNative401RefreshIsBounded(t *testing.T) {
	for _, repeated401 := range []bool{false, true} {
		t.Run(fmt.Sprintf("repeated_401=%t", repeated401), func(t *testing.T) {
			unauthorized := func() *http.Response {
				return bpsCompletionResponse(http.StatusUnauthorized, `{"detail":"Unauthorized"}`)
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{unauthorized()}, resp: unauthorized()}
			if repeated401 {
				upstream.responses = append(upstream.responses, unauthorized())
			} else {
				upstream.responses = append(upstream.responses, bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_refreshed", "answer")))
			}
			account := nativeRefreshTestAccount()
			account.Concurrency = 1
			account.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
			repo := &refreshAPIAccountRepo{account: account}
			executor := &refreshAPIExecutorStub{}
			// Every exchange produces a different grant, so repeated rejection
			// cannot accidentally stop just because a fake refresh reused a token.
			executor.onRefresh = func() {
				executor.credentials = map[string]any{
					"access_token":  fmt.Sprintf("native-refreshed-%d", executor.refreshCalls),
					"refresh_token": "native-refresh-next", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
				}
			}
			provider := NewOpenAITokenProvider(repo, nil, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), openAITestRefreshExecutor{executor})
			svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream, openAITokenProvider: provider}
			policyRepo := &rateLimitAccountRepoStub{}
			svc.rateLimitService = NewRateLimitService(policyRepo, nil, svc.cfg, nil, nil)
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
			payload := []byte(`{"type":"response.create","model":"gpt-6-astra","input":"hello"}`)
			var events [][]byte
			result, err := svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "old-at",
				payload, len(payload), "gpt-6-astra", "", "", "", "", 1,
				func(event []byte) error { events = append(events, append([]byte(nil), event...)); return nil })

			require.Equal(t, 1, executor.refreshCalls, "a single WS turn may force refresh only once")
			require.Zero(t, policyRepo.setErrorCalls, "generic bridge 401 must not permanently disable a refreshable account")
			require.Len(t, upstream.requests, 2)
			require.Equal(t, "Bearer old-at", upstream.requests[0].Header.Get("Authorization"))
			require.Equal(t, "Bearer native-refreshed-1", upstream.requests[1].Header.Get("Authorization"))
			require.Equal(t, upstream.bodies[0], upstream.bodies[1], "credential repair must preserve the request body")
			for _, request := range upstream.requests {
				require.Equal(t, "https://chatgpt.com/backend-api/codex/responses", request.URL.String())
			}
			if repeated401 {
				require.Equal(t, 1, policyRepo.tempCalls)
				require.Error(t, err)
				require.Nil(t, result)
				require.Empty(t, events, "the existing first-turn failover contract owns the final rejection")
			} else {
				require.Zero(t, policyRepo.tempCalls)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "resp_refreshed", result.RequestID)
				require.NotEmpty(t, events)
				require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
				for _, event := range events {
					require.NotEqual(t, "error", gjson.GetBytes(event, "type").String(), "the repaired pre-generation 401 must not leak to the client")
				}
			}
		})
	}
}

func TestOpenAIWSHTTPBridgeStreamFailuresDoNotRefreshOrReplay(t *testing.T) {
	for _, failure := range []string{"stream_401", "stream_403", "stream_401_after_output", "stream_403_after_output", "read_error_after_output"} {
		t.Run(failure, func(t *testing.T) {
			prefix := ""
			if strings.HasSuffix(failure, "after_output") {
				prefix = `data: {"type":"response.output_text.delta","response_id":"resp_partial_auth","delta":"partial"}` + "\n\n"
			}
			var body io.Reader
			if failure == "read_error_after_output" {
				body = io.MultiReader(strings.NewReader(prefix), iotest.ErrReader(io.ErrUnexpectedEOF))
			} else {
				status := http.StatusUnauthorized
				if strings.Contains(failure, "403") {
					status = http.StatusForbidden
				}
				body = strings.NewReader(prefix + fmt.Sprintf(`data: {"type":"response.failed","response":{"id":"resp_partial_auth","status":"failed","error":{"status_code":%d,"code":"unauthorized","message":"access token rejected"},"usage":{"input_tokens":7,"output_tokens":2}}}`, status) + "\n\n")
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(body),
			}}
			account := nativeRefreshTestAccount()
			repo := &refreshAPIAccountRepo{account: account}
			executor := &refreshAPIExecutorStub{credentials: map[string]any{"access_token": "must-not-be-issued", "refresh_token": "next"}}
			provider := NewOpenAITokenProvider(repo, nil, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), openAITestRefreshExecutor{executor})
			svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream, openAITokenProvider: provider}
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
			payload := []byte(`{"type":"response.create","model":"gpt-6-astra","input":"hello"}`)
			var events [][]byte
			result, err := svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "old-at",
				payload, len(payload), "gpt-6-astra", "", "", "", "", 1,
				func(event []byte) error { events = append(events, append([]byte(nil), event...)); return nil })
			require.Zero(t, executor.refreshCalls, "stream failures are not pre-generation HTTP credential rejections")
			require.Len(t, upstream.requests, 1)
			var streamFailover *UpstreamFailoverError
			require.False(t, errors.As(err, &streamFailover), "accepted stream authentication errors cannot authorize replay")
			if failure != "read_error_after_output" {
				require.NotNil(t, result)
				require.Equal(t, 7, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				require.Equal(t, openAIResponsesUpstreamEndpoint, result.UpstreamEndpoint)
				require.Equal(t, "response.failed", result.UpstreamTerminalEvent)
			}
			if strings.HasSuffix(failure, "after_output") {
				require.NotEmpty(t, events)
				require.Equal(t, "response.output_text.delta", gjson.GetBytes(events[0], "type").String())
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover), "already delivered semantic output must not permit replay")
			}
		})
	}
}

func TestOpenAIWSHTTPBridgeRevokedCredentialDoesNotRefresh(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusUnauthorized, `{"detail":"Unauthorized","error":{"code":"token_revoked"}}`)}
	account := nativeRefreshTestAccount()
	repo := &refreshAPIAccountRepo{account: account}
	executor := &refreshAPIExecutorStub{}
	provider := NewOpenAITokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), openAITestRefreshExecutor{executor})
	policyRepo := &rateLimitAccountRepoStub{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream, openAITokenProvider: provider}
	svc.rateLimitService = NewRateLimitService(policyRepo, nil, svc.cfg, nil, nil)
	ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
	payload := []byte(`{"type":"response.create","model":"gpt-6-astra","input":"hello"}`)
	_, err := svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "old-at", payload, len(payload), "gpt-6-astra", "", "", "", "", 1, func([]byte) error { return nil })
	require.Error(t, err)
	require.Zero(t, executor.refreshCalls)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, 1, policyRepo.setErrorCalls)
	require.Zero(t, policyRepo.tempCalls)
}
