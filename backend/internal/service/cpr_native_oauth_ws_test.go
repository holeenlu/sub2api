package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The real socket transport observes its actual localhost handshake. Only the
// test dial destination is substituted; no production URL or credential is used.
type cprOAuthLocalDialer struct {
	t        *testing.T
	endpoint string
	dialer   openAIWSClientDialer
}

func (d *cprOAuthLocalDialer) Dial(ctx context.Context, target string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	require.True(d.t, strings.HasPrefix(target, "wss://chatgpt.com/"), "the HTTP consumer really selected the OAuth WS route")
	return d.dialer.Dial(ctx, d.endpoint, headers, proxy)
}

func TestCPRNativeOAuthHTTPToWSActualConsumer(t *testing.T) {
	var frames atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}
		frames.Add(1)
		response, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{
			"id": "resp-cpr-oauth", "model": "gpt-5.4", "status": "completed", "output": []any{},
			"usage": map[string]int{"input_tokens": 2, "output_tokens": 1},
		}})
		_ = conn.Write(r.Context(), coderws.MessageText, response)
	}))
	t.Cleanup(peer.Close)
	f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, peer.URL)
	f.resource.Type = AccountTypeOAuth
	f.resource.Credentials["access_token"] = "cpr-synthetic-oauth"
	f.resource.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	f.resource.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	f.service.cfg.Gateway.OpenAIWS.OAuthEnabled = true
	f.service.openAITokenProvider = NewOpenAITokenProvider(f.service.accountRepo, nil, nil)
	pool := f.service.getOpenAIWSConnPool()
	pool.setClientDialerForTest(&cprOAuthLocalDialer{t: t, endpoint: "ws" + strings.TrimPrefix(peer.URL, "http"), dialer: newDefaultOpenAIWSClientDialer()})
	body, err := json.Marshal(map[string]any{"model": "client-model", "input": "hello", "stream": false})
	require.NoError(t, err)
	request := f.op.request
	request.OperationID = "cpr-oauth-http-ws"
	ctx, err := f.service.BindNativeHTTPDispatch(EnsureOpenAITurnBudget(t.Context(), f.service.cfg), &APIKeyService{apiKeyRepo: f.keys}, request)
	require.NoError(t, err)
	ctx, err = f.service.BindNativeHTTPOperation(ctx, model.ProtocolResponses, sha256.Sum256(body))
	require.NoError(t, err)
	ctx = bindNativeWSConsumerPricing(t, f, ctx)
	user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
	require.NoError(t, err)
	defer user.ReleaseFunc()
	account, err := routing.AcquireCapacity(ctx, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: "cpr-http-ws", Limit: f.resource.Concurrency}, nil)
	require.NoError(t, err)
	require.True(t, account.Acquired)
	defer account.ReleaseFunc()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body))).WithContext(ctx)
	c.Set("api_key", f.key)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	err = RunNativeHTTPOperation(ctx, c, func() NativeOperationStep {
		result, forwardErr := f.service.Forward(ctx, c, f.resource, body)
		require.NoError(t, forwardErr)
		require.True(t, result.OpenAIWSMode)
		handled, finalizeErr := finalizeNativeHTTPUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource}, nativeWSFixtureJournal(), time.Second)
		require.True(t, handled)
		require.NoError(t, finalizeErr)
		return NativeOperationStep{Err: forwardErr}
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, frames.Load())
	require.Zero(t, f.transport.sends.Load(), "no HTTP fallback or duplicate generation")
	require.Equal(t, "http", nativeDispatchFromContext(ctx).request.Transport)
	require.Len(t, nativeHTTPFromContext(ctx).core.Snapshot().Attempts, 1)
}
