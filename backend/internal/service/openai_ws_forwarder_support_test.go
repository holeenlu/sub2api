package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
)

func TestOpenAIWSSemantic429UsesCurrentEventHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, headers, want string
	}{
		{name: "current", headers: `{"Retry-After":"67","Authorization":"private-secret"}`, want: "67"},
		{name: "lowercase", headers: `{"retry-after":"67"}`, want: "67"},
		{name: "missing", headers: `{}`},
		{name: "invalid", headers: `{"Retry-After":"Inf"}`},
		{name: "number", headers: `{"Retry-After":67}`},
		{name: "duplicate", headers: `{"Retry-After":"67","retry-after":"1"}`},
		{name: "same case duplicate", headers: `{"Retry-After":"67","Retry-After":"1"}`},
		{name: "line break", headers: `{"Retry-After":"67\r\nX-Other: x"}`},
		{name: "leading CRLF", headers: `{"Retry-After":"\r\n67"}`},
		{name: "trailing CRLF", headers: `{"Retry-After":"67\r\n"}`},
		{name: "oversized whitespace", headers: `{"Retry-After":"` + strings.Repeat(" ", 127) + `67"}`},
		{name: "bounded whitespace", headers: `{"Retry-After":" 67 "}`, want: "67"},
		{name: "oversized", headers: `{"Retry-After":"` + strings.Repeat("1", 129) + `"}`},
	} {
		for _, model := range []string{"gpt-6-astra", "gpt-5.3-codex-spark"} {
			t.Run(tc.name+"/"+model, func(t *testing.T) {
				account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				handshake := http.Header{"Retry-After": {"7200"}, "X-Codex-Primary-Used-Percent": {"100"}, "X-Codex-Primary-Reset-After-Seconds": {"7200"}, "X-Codex-Primary-Window-Minutes": {"10080"}}
				original := handshake.Clone()
				body := []byte(`{"type":"error","status":429,"headers":` + tc.headers + `,"error":{"code":"turn_rate_limit_exceeded","type":"rate_limit_error","message":"rate limit exceeded"}}`)
				headers := openAIWSSemantic429Headers(account, model, handshake, body)
				require.Equal(t, tc.want, headers.Get("Retry-After"))
				require.Empty(t, headers.Get("Authorization"))
				if isCodexSparkModel(model) {
					require.Equal(t, "100", headers.Get("X-Codex-Primary-Used-Percent"))
				} else {
					require.Empty(t, headers.Get("X-Codex-Primary-Used-Percent"))
					svc := &OpenAIGatewayService{}
					failure := svc.newOpenAIWSRateLimitFailoverError(account, handshake, body, "rate limit exceeded", model)
					require.Equal(t, http.StatusTooManyRequests, failure.StatusCode)
					require.True(t, failure.RetryableOnSameAccount)
					wantDelay := openAIOAuth429RetryDelay
					if tc.want != "" {
						wantDelay = 67 * time.Second
					}
					require.Equal(t, wantDelay, failure.SameAccountRetryDelay)
					require.Equal(t, tc.want, failure.ResponseHeaders.Get("Retry-After"))
				}
				require.Equal(t, original, handshake, "do not mutate a pooled connection's handshake snapshot")
			})
		}
	}
	t.Run("actual handshake rejection keeps headers", func(t *testing.T) {
		svc := &OpenAIGatewayService{}
		headers := http.Header{"Retry-After": {"67"}}
		failure := svc.newOpenAIWSRateLimitFailoverError(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, headers, nil, "rate limit exceeded")
		require.Equal(t, "67", failure.ResponseHeaders.Get("Retry-After"))
		require.Equal(t, 67*time.Second, failure.SameAccountRetryDelay)
	})
	t.Run("absent event cannot inherit handshake retry delay", func(t *testing.T) {
		headers := http.Header{"Retry-After": {"7200"}, "X-Codex-Primary-Used-Percent": {"100"}}
		selected := openAIWSSemantic429Headers(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, "gpt-5.3-codex-spark", headers)
		require.Empty(t, selected.Get("Retry-After"))
		require.Equal(t, "100", selected.Get("X-Codex-Primary-Used-Percent"))
		require.Equal(t, "7200", headers.Get("Retry-After"))
	})
}

// Complement the existing V2 consumer matrix with a terminal lacking usage,
// pre-output cancellation and invalid event-header bytes. The peer is local;
// the pool, operation, dispatch authority and routing receipts are real.
func TestOpenAIWSV2Semantic429ConsumerRecoveryBoundaries(t *testing.T) {
	const terminal = `{"type":"response.completed","response":{"id":"resp_tail","status":"completed","output":[]}}`
	for _, tc := range []struct {
		name, headers, tail, wantRetry string
		cancelClient                   bool
	}{
		{name: "current event", headers: `{"Retry-After":"67"}`, wantRetry: "67"},
		{name: "no event headers", headers: `{}`},
		{name: "CRLF rejected", headers: `{"Retry-After":"\r\n67\r\n"}`},
		{name: "padded oversized rejected", headers: `{"Retry-After":"` + strings.Repeat(" ", 127) + `67"}`},
		{name: "same frame unmetered terminal", headers: `{"Retry-After":"67"}`, tail: terminal},
		{name: "canceled before rejection", headers: `{"Retry-After":"67"}`, cancelClient: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cancel context.CancelFunc
			var sends atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "7200")
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				if _, _, err = conn.Read(r.Context()); err != nil {
					return
				}
				sends.Add(1)
				if tc.cancelClient {
					cancel()
				}
				event := `{"type":"error","status":429,"headers":` + tc.headers + `,"error":{"code":"turn_rate_limit_exceeded","message":"rate limit exceeded"}}`
				_ = conn.Write(r.Context(), coderws.MessageText, []byte(event+tc.tail))
			}))
			defer peer.Close()
			f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, peer.URL)
			bindWSRecoveryIntentFixture(t, f.service, f.ctx, f.key)
			clientCtx, stop := context.WithCancel(f.ctx)
			cancel = stop
			defer cancel()
			user, err := routing.AcquireCapacity(f.ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
			require.NoError(t, err)
			defer user.ReleaseFunc()
			account, err := routing.AcquireCapacity(f.ctx, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: tc.name, Limit: 2}, nil)
			require.NoError(t, err)
			require.True(t, account.Acquired)
			defer account.ReleaseFunc()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(clientCtx)
			c.Set("api_key", f.key)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			owner := nativeHTTPFromContext(f.ctx)
			var result *OpenAIForwardResult
			err = owner.core.Execute(clientCtx, func() NativeOperationStep {
				var forwardErr error
				result, forwardErr = f.service.forwardOpenAIWSV2(clientCtx, c, f.resource,
					map[string]any{"model": "client-model", "input": "hello", "stream": true}, "", "semantic-429-consumer", "synthetic-upstream-key",
					OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, true, "client-model", "client-model", time.Now(), 1, time.Time{}, "", nil)
				return NativeOperationStep{Err: forwardErr}
			})
			require.Error(t, err)
			require.EqualValues(t, 1, sends.Load())
			require.Zero(t, f.transport.sends.Load(), "semantic 429 cannot switch to HTTP")
			require.Zero(t, f.transport.legacy.Load())
			require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
			facts := owner.core.Snapshot()
			require.Len(t, facts.Attempts, 1)
			require.Equal(t, model.SendSent, facts.Attempts[0].Send)
			var failover *UpstreamFailoverError
			if tc.cancelClient || tc.tail != "" {
				require.False(t, errors.As(err, &failover))
				var fallback *openAIWSFallbackError
				require.False(t, errors.As(err, &fallback))
				if tc.cancelClient {
					require.Empty(t, rec.Body.String())
					require.NotNil(t, result)
					require.True(t, result.ClientDisconnect)
				} else {
					require.Contains(t, rec.Body.String(), "turn_rate_limit_exceeded")
					require.NotContains(t, rec.Body.String(), "response.completed")
					require.Equal(t, "resp_tail", facts.Attempts[0].ResponseID)
					require.Equal(t, model.TerminalCompleted, facts.Attempts[0].Terminal)
				}
			} else {
				require.ErrorAs(t, err, &failover)
				require.Equal(t, http.StatusTooManyRequests, failover.StatusCode)
				require.Equal(t, tc.wantRetry, failover.ResponseHeaders.Get("Retry-After"))
				require.False(t, failover.RequestNotSent)
				require.Empty(t, rec.Body.String())
			}
		})
	}
}
