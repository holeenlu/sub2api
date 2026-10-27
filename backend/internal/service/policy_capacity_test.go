//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type policyCapacityRepo struct {
	AccountRepository
	mu        sync.Mutex
	deadlines map[int64]int64
	err       error
}

func (r *policyCapacityRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.deadlines == nil {
		r.deadlines = make(map[int64]int64)
	}
	if until, ok := updates[PolicyCapacityUnavailableUntilKey].(int64); ok && until > r.deadlines[id] {
		r.deadlines[id] = until
	}
	return nil
}
func (r *policyCapacityRepo) deadline(id int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deadlines[id]
}
func policyCapacityPayload(kind, code string) []byte {
	failure := map[string]any{"code": code, "message": "Policy-restricted capacity is unavailable. Please try again later."}
	event := map[string]any{"error": failure}
	if kind != "" {
		event["type"] = kind
	}
	if kind == "response.failed" {
		delete(event, "error")
		event["response"] = map[string]any{"id": "resp_policy", "status": "failed", "error": failure, "usage": map[string]any{"input_tokens": 3, "output_tokens": 0}}
	}
	out, _ := json.Marshal(event)
	return out
}
func TestPolicyCapacityStructuredErrorsOnly(t *testing.T) {
	for _, kind := range []string{"", "error", "response.failed"} {
		require.True(t, isPolicyCapacityUnavailable(policyCapacityPayload(kind, "policy_capacity_unavailable")), kind)
		for _, code := range []string{"server_is_overloaded", "slow_down", "rate_limit_exceeded", "policyCapacityUnavailable", "policy_capacity_unavailable_extra"} {
			require.False(t, isPolicyCapacityUnavailable(policyCapacityPayload(kind, code)), code)
		}
	}
	for _, raw := range []string{
		"policy_capacity_unavailable",
		"{\"error\":{\"message\":\"policy_capacity_unavailable\"}}",
		"{\"type\":\"response.output_text.delta\",\"delta\":\"policy_capacity_unavailable\"}",
		"{\"type\":\"response.completed\",\"error\":{\"code\":\"policy_capacity_unavailable\"}}",
		"{\"error\":{\"code\":\"policy_capacity_unavailable\"}}garbage",
	} {
		require.False(t, isPolicyCapacityUnavailable([]byte(raw)), raw)
	}
	payload := policyCapacityPayload("error", "policy_capacity_unavailable")
	unchanged, modified := sanitizeOpenAICapacityShedErrorCodeForClient(payload)
	require.False(t, modified)
	require.Equal(t, payload, unchanged)
}
func TestPolicyCapacityRenewalExpiryAndCanceledRequest(t *testing.T) {
	repo := &policyCapacityRepo{}
	account := &Account{ID: 42}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := time.Now().Add(time.Hour).UnixMilli()
	recordPolicyCapacityUnavailable(ctx, repo, account, policyCapacityPayload("error", "policy_capacity_unavailable"))
	first := repo.deadline(42)
	require.GreaterOrEqual(t, first, before)
	require.LessOrEqual(t, first, time.Now().Add(time.Hour).UnixMilli())
	require.Nil(t, account.Extra, "do not race by mutating scheduler-owned snapshots")
	observed := &Account{Extra: map[string]any{PolicyCapacityUnavailableUntilKey: float64(first)}}
	require.NotNil(t, observed.PolicyCapacityUnavailableUntil(time.UnixMilli(first-1)))
	require.Nil(t, observed.PolicyCapacityUnavailableUntil(time.UnixMilli(first)))
	require.Nil(t, observed.PolicyCapacityUnavailableUntil(time.UnixMilli(first+1)))
	recordPolicyCapacityUnavailable(ctx, repo, account, policyCapacityPayload("error", "server_is_overloaded"))
	require.Equal(t, first, repo.deadline(42), "other errors never refresh")
	repo.deadlines[42] = first - 1000
	recordPolicyCapacityUnavailable(ctx, repo, account, policyCapacityPayload("response.failed", "policy_capacity_unavailable"))
	require.GreaterOrEqual(t, repo.deadline(42), first)
	require.Zero(t, repo.deadline(99), "only the emitting account is marked")
	repo.err = errors.New("storage unavailable")
	require.NotPanics(t, func() {
		recordPolicyCapacityUnavailable(ctx, repo, account, policyCapacityPayload("error", "policy_capacity_unavailable"))
	})
}
func TestPolicyCapacityNativeResponsePaths(t *testing.T) {
	for _, path := range []string{"native_sse", "passthrough_sse", "native_buffered", "passthrough_buffered", "chat_sse", "chat_buffered", "messages_sse", "messages_buffered"} {
		t.Run(path, func(t *testing.T) {
			repo := &policyCapacityRepo{}
			svc, c, account, _ := incrementalFixture(t)
			svc.accountRepo = repo
			svc.cfg = &config.Config{}
			wire := "data: " + string(policyCapacityPayload("error", "policy_capacity_unavailable")) + "\n\n" +
				"data: " + string(policyCapacityPayload("response.failed", "policy_capacity_unavailable")) + "\n\n"
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(bytes.NewBufferString(wire))}
			switch path {
			case "chat_sse":
				_, _ = svc.handleChatStreamingResponse(resp, c, account, "gpt-5.5", "gpt-5.5", "gpt-5.5", time.Now(), 0)
			case "chat_buffered":
				_, _ = svc.handleChatBufferedStreamingResponse(resp, c, account, "gpt-5.5", "gpt-5.5", "gpt-5.5", time.Now())
			case "messages_sse":
				_, _ = svc.handleAnthropicStreamingResponse(resp, c, account, "gpt-5.5", "gpt-5.5", "gpt-5.5", time.Now())
			case "messages_buffered":
				_, _ = svc.handleAnthropicBufferedStreamingResponse(resp, c, account, "gpt-5.5", "gpt-5.5", "gpt-5.5", time.Now())
			case "native_sse":
				_, _ = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
			case "passthrough_sse":
				_, _ = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
			case "native_buffered":
				_, _ = svc.handleNonStreamingResponse(c.Request.Context(), resp, c, account, "gpt-5.5", "gpt-5.5")
			case "passthrough_buffered":
				_, _ = svc.handleNonStreamingResponsePassthrough(c.Request.Context(), resp, c, account, "gpt-5.5", "gpt-5.5")
			}
			require.Greater(t, repo.deadline(account.ID), time.Now().UnixMilli())
		})
	}
}
func TestPolicyCapacityBPSHTTPAndStreaming(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			payload := policyCapacityPayload("", "policy_capacity_unavailable")
			headers := http.Header{"Content-Type": []string{"application/json"}}
			if status == 200 {
				payload = []byte("data: " + string(policyCapacityPayload("response.failed", "policy_capacity_unavailable")) + "\n\n")
				headers.Set("Content-Type", "text/event-stream")
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(bytes.NewReader(payload))}}
			svc := excelBPSGatewayTestService(upstream)
			repo := &policyCapacityRepo{}
			svc.accountRepo = repo
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			body, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": true, "input": "test"})
			account := excelAccount()
			_, _ = svc.Forward(context.Background(), c, account, body)
			require.Greater(t, repo.deadline(account.ID), time.Now().UnixMilli())
		})
	}
}
func TestPolicyCapacityCloudTerminalIgnoresReplay(t *testing.T) {
	repo := &policyCapacityRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 43}
	payload := policyCapacityPayload("", "policy_capacity_unavailable")
	var response map[string]any
	require.NoError(t, json.Unmarshal(payload, &response))
	response["status"] = "failed"
	response["error"].(map[string]any)["upstream_http_status"] = 503
	payload, _ = json.Marshal(response)
	result := &OpenAICloudResult{Response: payload, ErrorStatus: 503, Replayed: true, Terminal: true}
	svc.applyCloudTerminalAccountPolicy(context.Background(), account, "gpt-5.5", result)
	require.Zero(t, repo.deadline(43))
	result.Replayed = false
	svc.applyCloudTerminalAccountPolicy(context.Background(), account, "gpt-5.5", result)
	require.Greater(t, repo.deadline(43), time.Now().UnixMilli())
}

func TestPolicyCapacityWebSocketPassthrough(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	upstream := newStagedPassthroughConn()
	upstream.Send(string(policyCapacityPayload("response.failed", "policy_capacity_unavailable")))
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	repo := &policyCapacityRepo{}
	svc.accountRepo = repo
	account := passthroughLifecycleAccount()
	server, serverErr := startPassthroughLifecycleServer(t, ctx, svc, account)
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer client.CloseNow()
	_, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Greater(t, repo.deadline(account.ID), time.Now().UnixMilli())
	_ = client.CloseNow()
	cancel(context.Canceled)
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("websocket did not stop")
	}
}
func TestPolicyCapacityHTTPAccountError(t *testing.T) {
	repo := &policyCapacityRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	svc.handleOpenAIAccountUpstreamError(context.Background(), account, 503, nil, policyCapacityPayload("", "policy_capacity_unavailable"))
	require.Greater(t, repo.deadline(44), time.Now().UnixMilli())
}

func TestPolicyCapacityHTTPWebSocketBridge(t *testing.T) {
	repo := &policyCapacityRepo{}
	wire := "data: " + string(policyCapacityPayload("response.failed", "policy_capacity_unavailable")) + "\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(wire))}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: repo}
	account := &Account{ID: 45, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	payload, _ := json.Marshal(map[string]any{"type": "response.create", "model": "gpt-5", "input": "hi"})
	var writes [][]byte
	_, _ = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "sk-test", payload, len(payload), "gpt-5", "", "", "", "", 2, func(message []byte) error { writes = append(writes, append([]byte(nil), message...)); return nil })
	require.Greater(t, repo.deadline(account.ID), time.Now().UnixMilli())
	require.NotEmpty(t, writes)
	require.Contains(t, string(writes[len(writes)-1]), "policy_capacity_unavailable")
}

func TestPolicyCapacityCloudRetryNoticeUsesEventOwner(t *testing.T) {
	actor, _, _ := cloudHostedActorForTest(t)
	observed := 0
	actor.service.observeUpstreamError = func(_ context.Context, owner OpenAICloudOwner, failure *cloudUpstreamError) {
		require.Equal(t, actor.record.AccountID, owner.AccountID)
		require.Equal(t, "policyCapacityUnavailable", failure.CodexErrorInfo)
		observed++
	}
	fields := map[string]any{"willRetry": true, "error": map[string]any{"codexErrorInfo": "policyCapacityUnavailable"}}
	require.NoError(t, cloudHostedEventForTest(t, actor, "error", fields, ""))
	require.Equal(t, 1, observed)
	require.Empty(t, actor.publicFailure, "a retry notice must not terminate the turn")
	fields["threadId"] = "foreign"
	require.NoError(t, cloudHostedEventForTest(t, actor, "error", fields, ""))
	require.Equal(t, 1, observed, "foreign events cannot mark this account")
}
