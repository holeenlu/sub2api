package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Real local WS sockets and the service Forward consumer exercise producing scope,
// actual pool receipts and send permits. Billing uses the explicit existing
// unit journal acknowledgement; this does not claim real money persistence.
// Transport choice is explicit at the service fixture. This does not claim that
// public API-key HTTP ingress selects WS; that ingress preserves its HTTP path.
func TestNativeForwardSelectedWSProducerScopeSurvivesConsumerStoreChanges(t *testing.T) {
	for _, tc := range []struct {
		name          string
		producerStore bool
		consumerStore bool
		advance       bool
		loseSocket    bool
		denied        bool
	}{
		{"local_same_generation", false, true, false, false, false},
		{"local_other_write_generation", false, true, true, false, true},
		{"local_socket_lost", false, true, false, true, true},
		{"persisted_new_socket", true, false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var creates, dials atomic.Int32
			observed := make(chan []byte, 8)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				socket, err := coderws.Accept(w, req, nil)
				if err != nil {
					return
				}
				dials.Add(1)
				defer socket.CloseNow()
				for {
					_, body, err := socket.Read(req.Context())
					if err != nil {
						return
					}
					if gjson.GetBytes(body, "type").String() != "response.create" {
						continue
					}
					n := creates.Add(1)
					observed <- body
					response := fmt.Sprintf("{\"type\":\"response.completed\",\"response\":{\"id\":\"response-%d\",\"model\":\"client-model\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}", n)
					if socket.Write(req.Context(), coderws.MessageText, []byte(response)) != nil {
						return
					}
				}
			}))
			t.Cleanup(upstream.Close)
			f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, upstream.URL)
			f.service.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			f.service.cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			run := func(number int, stored bool, previous string) (*OpenAIForwardResult, error) {
				body := fmt.Sprintf("{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":false,\"store\":%t", stored)
				if previous != "" {
					body += ",\"previous_response_id\":\"" + previous + "\""
				}
				body += "}"
				request := f.op.request
				request.OperationID = fmt.Sprintf("scope-%s-%d", tc.name, number)
				ctx, err := f.service.BindNativeHTTPDispatch(EnsureOpenAITurnBudget(t.Context(), f.service.cfg), &APIKeyService{apiKeyRepo: f.keys}, request)
				require.NoError(t, err)
				ctx, err = f.service.BindNativeHTTPOperation(ctx, model.ProtocolResponses, sha256.Sum256([]byte(body)))
				require.NoError(t, err)
				ctx = context.WithValue(ctx, ctxkey.RequestID, request.OperationID)
				ctx = bindNativeWSConsumerPricing(t, f, ctx)
				user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
				require.NoError(t, err)
				defer user.ReleaseFunc()
				account, err := routing.AcquireCapacity(ctx, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: request.OperationID, Limit: 2}, nil)
				require.NoError(t, err)
				require.True(t, account.Acquired)
				defer account.ReleaseFunc()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)).WithContext(ctx)
				c.Request.Header.Set("Session_id", "same-client-window")
				c.Set("api_key", f.key)
				var result *OpenAIForwardResult
				runErr := RunNativeHTTPOperation(ctx, c, func() NativeOperationStep {
					var forwardErr error
					result, forwardErr = TrackNativeHTTPForward(ctx, func() (*OpenAIForwardResult, error) {
						return f.service.Forward(ctx, c, f.resource, []byte(body))
					})
					handled, finalErr := finalizeNativeHTTPUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource}, nativeWSFixtureJournal(), time.Second)
					if number == 2 && tc.denied {
						require.False(t, handled, "a rejected continuation with no send has no usage to finalize")
						require.Empty(t, nativeHTTPFromContext(ctx).core.Snapshot().Attempts)
					} else {
						require.True(t, handled)
					}
					require.NoError(t, finalErr)
					return NativeOperationStep{Err: forwardErr}
				})
				require.Zero(t, f.transport.sends.Load(), "local scope rejection cannot silently move to HTTP")
				require.Zero(t, f.transport.legacy.Load())
				return result, runErr
			}
			first, err := run(1, tc.producerStore, "")
			require.NoError(t, err)
			require.NotNil(t, first)
			require.Equal(t, "response-1", first.RequestID)
			wsLifecycleReceive(t, observed)
			store := f.service.getOpenAIWSStateStore()
			connID, scope, found := store.GetResponseContinuation(first.RequestID)
			require.True(t, found)
			require.Equal(t, openAIWSResponseScopeForStore(!tc.producerStore), scope)
			pool := f.service.getOpenAIWSConnPool()
			ap := pool.getOrCreateAccountPool(f.resource.ID)
			ap.mu.Lock()
			conn := ap.conns[connID]
			ap.mu.Unlock()
			require.NotNil(t, conn)
			if tc.advance {
				require.True(t, conn.tryAcquire())
				otherLane := &openAIWSConnLease{pool: pool, accountID: f.resource.ID, conn: conn}
				controlCtx, cancel := context.WithTimeout(t.Context(), time.Second)
				controlErr := otherLane.WriteJSONContext(controlCtx, map[string]string{"type": "response.cancel"})
				cancel()
				otherLane.Release()
				require.NoError(t, controlErr)
			}
			if tc.loseSocket {
				pool.evictConn(f.resource.ID, connID)
				store.DeleteResponseConn(first.RequestID, connID)
				lostConn, retainedScope, retained := store.GetResponseContinuation(first.RequestID)
				require.True(t, retained)
				require.Empty(t, lostConn)
				require.Equal(t, scope, retainedScope)
			}
			second, err := run(2, tc.consumerStore, first.RequestID)
			if tc.denied {
				require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
				require.Nil(t, second)
				require.EqualValues(t, 1, creates.Load())
				require.EqualValues(t, 1, dials.Load())
			} else {
				require.NoError(t, err)
				require.NotNil(t, second)
				require.EqualValues(t, 2, creates.Load())
				require.Equal(t, "response-2", second.RequestID)
				wire := wsLifecycleReceive(t, observed)
				require.Equal(t, "response-1", gjson.GetBytes(wire, "previous_response_id").String())
				if tc.loseSocket {
					require.EqualValues(t, 2, dials.Load())
				} else {
					require.EqualValues(t, 1, dials.Load())
				}
			}
		})
	}
}

func TestNativeWSIngressPublishesProducingScope(t *testing.T) {
	for _, stored := range []bool{false, true} {
		t.Run(fmt.Sprint(stored), func(t *testing.T) {
			var creates atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				socket, err := coderws.Accept(w, req, nil)
				if err != nil {
					return
				}
				defer socket.CloseNow()
				for {
					_, body, err := socket.Read(req.Context())
					if err != nil {
						return
					}
					if gjson.GetBytes(body, "type").String() != "response.create" {
						continue
					}
					creates.Add(1)
					if socket.Write(req.Context(), coderws.MessageText, []byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"ingress-parent\",\"status\":\"completed\",\"model\":\"client-model\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}")) != nil {
						return
					}
				}
			}))
			t.Cleanup(upstream.Close)
			f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, upstream.URL)
			client, done := nativeWSAuditClient(t, f)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			payload := fmt.Sprintf("{\"type\":\"response.create\",\"model\":\"client-model\",\"input\":\"hello\",\"store\":%t}", stored)
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
			for {
				_, event, err := client.Read(ctx)
				require.NoError(t, err)
				if gjson.GetBytes(event, "type").String() == "response.completed" {
					break
				}
			}
			store := f.service.getOpenAIWSStateStore()
			var connID string
			require.Eventually(t, func() bool {
				id, scope, found := store.GetResponseContinuation("ingress-parent")
				if found && scope == openAIWSResponseScopeForStore(!stored) {
					connID = id
					return true
				}
				return false
			}, time.Second, time.Millisecond)
			require.NotEmpty(t, connID)
			require.EqualValues(t, 1, creates.Load())
			_ = client.Close(coderws.StatusNormalClosure, "complete")
			out := wsLifecycleReceive(t, done)
			require.Len(t, out.facts.Attempts, 1)
			require.Equal(t, model.SendSent, out.facts.Attempts[0].Send)
		})
	}
}
