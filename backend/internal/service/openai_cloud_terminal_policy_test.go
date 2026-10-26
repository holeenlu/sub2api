//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudTerminalUsesNativePolicyAcrossIngress(t *testing.T) {
	for _, transport := range []string{"json", "sse", "ws"} {
		t.Run(transport, func(t *testing.T) {
			svc, in, _, wire := cloudForwardFixture(t, nil)
			repo := &oauth429RateLimitRepo{}
			svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			svc.rateLimitService.SetAccountRuntimeBlocker(svc)
			svc.openaiOAuth429RetryStartedAt.Store(in.Account.ID, time.Now().Add(-openAIOAuth429RetryWindow-time.Second))
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			c.Set("api_key", in.APIKey)
			SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
			bindPassthroughRule(c, PlatformOpenAI, []string{"rate_limit_exceeded"}, http.StatusBadRequest)
			model := "client-model"
			if transport == "ws" {
				model = "gpt-5.1"
			}
			body, err := json.Marshal(map[string]any{"model": model, "input": "go", "stream": transport != "json"})
			require.NoError(t, err)
			done := make(chan struct{})
			defer func() { cancel(); <-done }()
			go func() {
				defer close(done)
				cloudWireStart(t, ctx, wire)
				// Observed output prevents this terminal from becoming a replay.
				cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "text", "type": "agentMessage"}}})
				cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "text", "delta": "partial"}})
				failure := map[string]any{"code": "rate_limit_exceeded", "httpStatusCode": 429, "message": "limited"}
				cloudTestRead(wire, map[string]any{"method": "error", "params": map[string]any{"threadId": "thread", "turnId": "turn", "willRetry": true, "error": failure}})
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "failed", "error": failure}}})
			}()
			var result *OpenAIForwardResult
			if transport == "ws" {
				result, err = svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, body, "client-model", func(event []byte) error { _, e := fmt.Fprintln(rec, string(event)); return e })
			} else {
				result, err = svc.forwardOpenAICloud(ctx, c, in.Account, body)
			}
			require.NoError(t, err)
			require.False(t, result.wsContextRejected, "display remapping does not turn the measured upstream failure into a request rejection")
			require.False(t, result.SchedulingOutcomeKnown(), "partial failure without final usage stays an unknown scheduling outcome")
			require.Equal(t, 1, repo.setRateLimitedCalls, "one accepted failed turn must apply the native account policy once")
			require.Equal(t, http.StatusTooManyRequests, c.GetInt(OpsUpstreamStatusCodeKey), "Ops keeps the original upstream status")
			if transport == "json" {
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, int64(http.StatusBadRequest), gjson.Get(rec.Body.String(), "error.status_code").Int())
			} else {
				require.Contains(t, rec.Body.String(), "\"status_code\":400", "the shared rule must also alter the terminal stream envelope")
			}
		})
	}
}

func TestCloudTerminalPolicyProtectsLocalAndReplayedFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     string
		method   string
		terminal bool
		replayed bool
		want     int
	}{
		{"upstream", "rate_limit_exceeded", "turn/completed", true, false, 1},
		{"replay", "rate_limit_exceeded", "turn/completed", true, true, 0},
		{"notice", "rate_limit_exceeded", "error", false, false, 0},
		{"local", "rate_limit_exceeded", "", true, false, 0},
		{"thread_budget", "cloud_session_budget_exhausted", "turn/completed", true, false, 0},
		{"local_queue", "cloud_event_queue_overflow", "turn/start", true, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &oauth429RateLimitRepo{}
			svc := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
			account := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			svc.openaiOAuth429RetryStartedAt.Store(account.ID, time.Now().Add(-openAIOAuth429RetryWindow-time.Second))
			raw, err := json.Marshal(map[string]any{"status": "failed", "error": map[string]any{"code": tc.code, "message": "limited", "upstream_method": tc.method}})
			require.NoError(t, err)
			svc.applyCloudTerminalAccountPolicy(t.Context(), account, "gpt-5.1", &OpenAICloudResult{Terminal: tc.terminal, Replayed: tc.replayed, ErrorStatus: 429, Response: raw})
			require.Equal(t, tc.want, repo.setRateLimitedCalls)
		})
	}
}

func TestCloudTerminalProjectionPreservesExecutionFacts(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	bindPassthroughRule(c, PlatformOpenAI, []string{"rate_limit_exceeded"}, http.StatusConflict)
	raw, err := json.Marshal(map[string]any{"status": "failed", "error": map[string]any{"code": "rate_limit_exceeded", "message": "limited", "status_code": 429, "upstream_http_status": 429, "upstream_method": "turn/completed"}})
	require.NoError(t, err)
	original := &OpenAICloudResult{Terminal: true, ErrorStatus: 429, Response: raw, RootRequestID: "root", ThreadID: "thread", TurnID: "turn", RejectedBeforeInference: true}
	projected := projectCloudTerminalError(c, &Account{Platform: PlatformOpenAI}, original)
	require.NotSame(t, original, projected)
	require.Equal(t, 429, original.ErrorStatus)
	require.Equal(t, raw, []byte(original.Response))
	require.Equal(t, http.StatusConflict, projected.ErrorStatus)
	require.Equal(t, int64(429), gjson.GetBytes(projected.Response, "error.upstream_http_status").Int())
	require.True(t, projected.RejectedBeforeInference)
	require.Equal(t, original.RootRequestID, projected.RootRequestID)
}

func TestCloudHandshakeFailureRetainsAccountPolicyFacts(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		a := &cloudSessionActor{}
		a.setPublicFailure(&cloudHandshakeError{StatusCode: status})
		require.Equal(t, status, a.publicFailure["upstream_http_status"])
	}
}

func TestCloudUnavailableTerminalIsNotAccountOutcome(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cause         error
		kind          string
		status        int
		requestScoped bool
	}{
		{"state", errCloudStateUnavailable, "api_error", http.StatusServiceUnavailable, false},
		{"history_item", &cloudGatewayError{413, "cloud_history_item_too_large", "large item"}, "invalid_request_error", http.StatusRequestEntityTooLarge, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range []string{"json", "sse", "ws"} {
				t.Run(transport, func(t *testing.T) {
					svc, in, _, _ := cloudForwardFixture(t, tc.cause)
					model := "client-model"
					if transport == "ws" {
						model = "gpt-5.1"
					}
					body, err := json.Marshal(map[string]any{"model": model, "input": "go", "stream": transport != "json"})
					require.NoError(t, err)
					ctx, c, rec := cloudContractContext(t, svc, in, t.Context(), body)
					var result *OpenAIForwardResult
					if transport == "ws" {
						result, err = svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, body, "client-model", func(event []byte) error { _, e := fmt.Fprintln(rec, string(event)); return e })
					} else {
						result, err = svc.forwardOpenAICloud(ctx, c, in.Account, body)
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.False(t, result.SchedulingOutcomeKnown(), "a local state failure is not a measured account outcome, even when Go error is nil")
					require.Contains(t, rec.Body.String(), "\"type\":\""+tc.kind+"\"")
					require.Zero(t, c.GetInt(OpsUpstreamStatusCodeKey), "local state lookup did not observe an upstream HTTP error")
					failure, ok := GetOpsStreamError(c)
					require.True(t, ok)
					require.Equal(t, tc.kind, failure.ErrType)
					require.Equal(t, tc.status, failure.IntendedStatus)
					require.Equal(t, tc.requestScoped, failure.RequestScoped)
					require.Equal(t, !tc.requestScoped, failure.CountTowardsSLA)
				})
			}
		})
	}
}

func TestCloudLocalFailureDoesNotPenalizeAccountScheduling(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 301, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	for range openAIHealthMinSamples {
		svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", errCloudStateUnavailable)
		svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", cloudBadRequest("input"))
		svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", &cloudGatewayError{413, "cloud_history_item_too_large", "large item"})
		svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", &cloudGatewayError{429, "cloud_session_budget_exhausted", "thread budget"})
		svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", &cloudGatewayError{409, "cloud_turn_conflict", "active turn"})
	}
	scheduler := svc.getOpenAIAccountScheduler(t.Context()).(*defaultOpenAIAccountScheduler)
	rate, _, _ := scheduler.stats.snapshot(account.ID, "gpt-6-astra")
	require.Zero(t, rate)
	for range openAIHealthMinSamples {
		svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", &cloudGatewayError{503, "cloud_upstream_error", "upstream failed"})
	}
	rate, _, _ = scheduler.stats.snapshot(account.ID, "gpt-6-astra")
	require.Greater(t, rate, 0.5)
}

func TestCloudBridgeRequestFailureRemainsRequestScoped(t *testing.T) {
	svc, in, _, _ := cloudForwardFixture(t, nil)
	body := []byte("{\"model\":\"gpt-5.1\",\"input\":\"go\"}")
	ctx, c, rec := cloudContractContext(t, svc, in, t.Context(), body)
	ctx = context.WithValue(ctx, cloudContinuationErrorKey{}, errCloudPreviousResponseNotFound)
	_, err := svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, body, "client-model", func(event []byte) error { _, e := fmt.Fprintln(rec, string(event)); return e })
	require.Error(t, err)
	failure, ok := GetOpsStreamError(c)
	require.True(t, ok)
	require.True(t, failure.RequestScoped)
	require.False(t, failure.CountTowardsSLA)
	require.Zero(t, failure.UpstreamStatus)
}

func TestCloudBackgroundToolFailureAppliesAccountPolicy(t *testing.T) {
	svc, in, store, wire := cloudForwardFixture(t, nil)
	repo := &oauth429RateLimitRepo{}
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc.rateLimitService.SetAccountRuntimeBlocker(svc)
	svc.openaiOAuth429RetryStartedAt.Store(in.Account.ID, time.Now().Add(-openAIOAuth429RetryWindow-time.Second))
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		cloudWireStart(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"id": "tool-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "call", "namespace": "sub2api_client", "tool": "echo", "arguments": map[string]any{}}})
	}()
	body, err := json.Marshal(map[string]any{"model": "client-model", "input": "go", "tools": []any{map[string]any{"type": "function", "name": "echo", "parameters": map[string]any{"type": "object"}}}})
	require.NoError(t, err)
	ctx, c, _ := cloudContractContext(t, svc, in, ctx, body)
	_, err = svc.forwardOpenAICloud(ctx, c, in.Account, body)
	require.NoError(t, err)
	require.Equal(t, "awaiting_tools", store.snapshot().Status)
	require.Zero(t, repo.setRateLimitedCalls)
	cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"code": "rate_limit_exceeded", "httpStatusCode": 429}}}})
	require.Eventually(t, func() bool { return store.snapshot().Status == "failed" }, 3*time.Second, 5*time.Millisecond)
	require.Equal(t, 1, repo.setRateLimitedCalls, "owner must handle the upstream terminal without a downstream request")
}
