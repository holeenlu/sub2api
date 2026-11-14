package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type nativeWSAuditOutcome struct {
	result    *OpenAIForwardResult
	facts     model.OperationResult
	err       error
	detail    string
	callbacks int
}

// Actual local WS ingress, the real dispatcher/core and routing receipts. The
// isolated journal port is not a PG or remote-provider acceptance claim.
func nativeWSAuditClient(t *testing.T, f *nativeWSConsumerFixture) (*coderws.Conn, <-chan nativeWSAuditOutcome) {
	t.Helper()
	done := make(chan nativeWSAuditOutcome, 1)
	var once sync.Once
	finish := func(out nativeWSAuditOutcome) { once.Do(func() { done <- out }) }
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := coderws.Accept(w, r, nil)
		if err != nil {
			finish(nativeWSAuditOutcome{err: err})
			return
		}
		defer socket.CloseNow()
		err = RunOpenAIWSMultiplexer(openAIWSTestAdmissionContext(r.Context()), socket, f.service.cfg, func(lane context.Context, ds *OpenAIWSDownstream) {
			defer ds.CloseNow()
			_, first, err := ds.Read(lane)
			if err != nil {
				finish(nativeWSAuditOutcome{err: err})
				return
			}
			request := f.op.request
			request.Transport = "ws"
			request.OperationID = "audit-" + gjson.GetBytes(first, "event_id").String()
			turn, err := f.service.BindNativeWSDispatch(ds.TurnBudgetContext(lane, f.service.cfg, first), &APIKeyService{apiKeyRepo: f.keys}, request)
			if err != nil {
				finish(nativeWSAuditOutcome{err: err})
				return
			}
			turn, err = f.service.BindNativeWSOperation(turn, first)
			if err != nil {
				finish(nativeWSAuditOutcome{err: err})
				return
			}
			turn = bindNativeWSConsumerPricing(t, f, turn)
			if err = ds.PublishNativeWSTurn(lane, turn); err != nil {
				finish(nativeWSAuditOutcome{err: err})
				return
			}
			user, err := routing.AcquireCapacity(turn, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
			if err != nil {
				finish(nativeWSAuditOutcome{err: err})
				return
			}
			defer user.ReleaseFunc()
			account, err := routing.AcquireCapacity(turn, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: request.OperationID, Limit: 2}, nil)
			if err != nil || !account.Acquired {
				finish(nativeWSAuditOutcome{err: fmt.Errorf("capacity: %v", err)})
				return
			}
			defer account.ReleaseFunc()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = r.Clone(turn)
			c.Set("api_key", f.key)
			out := nativeWSAuditOutcome{}
			hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, cause error) {
				out.callbacks++
				out.result = result
				_, _ = finalizeNativeWSUsage(turn, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource}, cause, nativeWSFixtureJournal(), time.Second)
			}, ReleaseTurn: func(int) { account.ReleaseFunc(); user.ReleaseFunc() }}
			out.err = f.service.ProxyResponsesWebSocketFromClient(turn, c, ds, f.resource, "synthetic-upstream-key", first, hooks)
			out.facts = nativeHTTPFromContext(turn).core.Snapshot()
			out.detail = c.GetString(OpsUpstreamErrorDetailKey)
			finish(out)
		})
		if err != nil {
			finish(nativeWSAuditOutcome{err: err})
		}
	}))
	t.Cleanup(downstream.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })
	return client, done
}

func TestNativeWSConsumerMeteredRateLimitDoesNotFailover(t *testing.T) {
	for _, tc := range []struct {
		name, usage  string
		input, cache int
	}{
		{"input_output", `{"input_tokens":5,"output_tokens":1}`, 5, 0},
		{"cache_only", `{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":3}}`, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var creates atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				if _, _, err = c.Read(r.Context()); err != nil {
					return
				}
				creates.Add(1)
				_ = c.Write(r.Context(), coderws.MessageText, []byte(`{"type":"error","error":{"code":"rate_limit_exceeded","type":"usage_limit_reached","message":"capacity exhausted after metering"},"usage":`+tc.usage+`}`))
			}))
			defer upstream.Close()
			f := nativeWSConsumerService(t, OpenAIWSIngressModePassthrough, upstream.URL)
			client, done := nativeWSAuditClient(t, f)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","event_id":"metered","model":"client-model","input":"hi"}`)))
			_, event, err := client.Read(ctx)
			require.NoError(t, err, "metered error must reach the reader/settlement instead of authorizing replay")
			require.Equal(t, "rate_limit_exceeded", gjson.GetBytes(event, "error.code").String())
			_ = client.CloseNow()
			out := wsLifecycleReceive(t, done)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(out.err, &failover))
			require.NotNil(t, out.result)
			require.True(t, out.result.Usage.HasTokens())
			require.Equal(t, tc.input, out.result.Usage.InputTokens)
			require.Equal(t, tc.cache, out.result.Usage.CacheReadInputTokens)
			require.Equal(t, 1, out.callbacks)
			require.EqualValues(t, 1, creates.Load())
			require.Zero(t, f.transport.sends.Load())
			require.Len(t, out.facts.Attempts, 1)
			require.NotNil(t, out.facts.Attempts[0].Usage)
			require.True(t, out.facts.Billing.Durable)
		})
	}
}

func TestNativeWSConsumerDeclinedRepairDetailSurvivesError(t *testing.T) {
	var creates atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		if _, _, err = c.Read(r.Context()); err != nil {
			return
		}
		creates.Add(1)
		_ = c.Write(r.Context(), coderws.MessageText, []byte(`{"type":"codex.rate_limits","rate_limits":{}}`))
		_ = c.Write(r.Context(), coderws.MessageText, []byte(`{"type":"error","error":{"code":"unsupported_parameter","type":"invalid_request_error","message":"This backend does not support persisted-item lookup.","param":"input[0].id"}}`))
	}))
	defer upstream.Close()
	f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, upstream.URL)
	client, done := nativeWSAuditClient(t, f)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","event_id":"declined","model":"client-model","input":[{"type":"item_reference","id":"msg_remote"}]}`)))
	for {
		_, event, err := client.Read(ctx)
		if err != nil {
			break
		}
		kind := gjson.GetBytes(event, "type").String()
		if kind == "response.failed" || kind == "error" {
			break
		}
	}
	_ = client.CloseNow()
	out := wsLifecycleReceive(t, done)
	require.Equal(t, "input repair declined: item_reference at input[0]", out.detail)
	require.EqualValues(t, 1, creates.Load())
	require.Len(t, out.facts.Attempts, 1)
}

func TestNativeWSLifecycleBridgeUnknownCanonicalStaysPerIntent(t *testing.T) {
	parent := context.WithValue(t.Context(), ctxkey.RequestID, "shared-connection")
	parent = context.WithValue(parent, ctxkey.ClientRequestID, "shared-client-correlation")
	ids := make(map[string]bool)
	for _, lane := range []string{"a", "b"} {
		f, _ := nativeWSLifecycleFixture(t, parent, "bridge-lane-"+lane)
		logs := &openAIRecordUsageLogRepoStub{inserted: true}
		journal := &nativeApplicationJournal{t: t}
		f.service.usageBillingRepo = journal
		bindNativeRuntimeFixture(t, f)
		f.service.usageLogRepo = logs
		f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"error\",\"error\":{\"code\":\"server_error\",\"message\":\"metered but no response identity\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"input_tokens_details\":{\"cached_tokens\":3}}}\n\n"))}, nil
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(f.ctx)
		c.Set("api_key", f.key)
		_, _, err := f.service.GetAccessToken(f.ctx, f.resource)
		require.NoError(t, err)
		payload := []byte(`{"type":"response.create","stream_id":"` + lane + `","event_id":"bridge-` + lane + `","model":"client-model","input":"hi"}`)
		var writes [][]byte
		result, cause := f.service.proxyOpenAIWSHTTPBridgeTurn(f.ctx, c, f.resource, "synthetic-upstream-key", payload, len(payload), "client-model", "", "", "", "", 1, func(b []byte) error { writes = append(writes, append([]byte(nil), b...)); return nil })
		require.Error(t, cause)
		require.NotNil(t, result, "bridge cause=%v; sends=%d; facts=%+v", cause, f.transport.sends.Load(), nativeHTTPFromContext(f.ctx).core.Snapshot())
		require.EqualValues(t, 1, f.transport.sends.Load(), "the registered bridge must reach exactly one HTTP transport attempt")
		require.Zero(t, f.transport.legacy.Load())
		facts := nativeHTTPFromContext(f.ctx).core.Snapshot()
		require.Equal(t, "bridge-lane-"+lane, facts.OperationID)
		require.Len(t, facts.Attempts, 1)
		require.Equal(t, "http", facts.Attempts[0].Identity.Transport)
		require.Equal(t, facts.OperationID, facts.Attempts[0].Identity.OperationID)
		require.Equal(t, model.SendSent, facts.Attempts[0].Send)
		require.True(t, facts.Attempts[0].Closed)
		require.Equal(t, "ws", f.op.request.Transport)
		require.Equal(t, "ws", f.op.authority.request.Transport)
		require.True(t, result.Usage.HasTokens())
		require.Equal(t, 3, result.Usage.CacheReadInputTokens)
		require.Empty(t, result.ResponseID)
		require.Empty(t, result.RequestID)
		// Missing canonical identity remains pending under the preserved intent.
		// A connection ID or generated UUID is not observed billing evidence.
		hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) {
			handled, err := f.service.FinalizeNativeWSUsage(f.ctx, &OpenAIRecordUsageInput{Result: r, APIKey: f.key, User: f.key.User, Account: f.resource}, e)
			require.True(t, handled)
			require.NoError(t, err)
		}}
		require.Error(t, completeOpenAIWSTurn(f.ctx, hooks, 1, result, cause))
		require.Equal(t, 1, journal.records)
		require.NotNil(t, journal.input)
		require.Nil(t, journal.input.Settlement)
		require.Equal(t, facts.OperationID, journal.input.Evidence.OperationID)
		require.NotNil(t, journal.input.Evidence.Usage)
		require.NotNil(t, journal.input.Evidence.Usage.CacheReadTokens)
		require.EqualValues(t, 3, *journal.input.Evidence.Usage.CacheReadTokens)
		finished := nativeHTTPFromContext(f.ctx).core.Snapshot()
		require.True(t, finished.Billing.Durable)
		require.Equal(t, model.BillingPendingUsage, finished.Billing.Disposition)
		require.Equal(t, model.OperationFailed, finished.State)
		require.Zero(t, logs.calls)
		require.Zero(t, journal.old)
		require.Empty(t, journal.input.Evidence.Identity.RequestID)
		require.NotNil(t, journal.final)
		id := journal.final.Intent.Key.OperationID
		require.Equal(t, facts.OperationID, id)
		require.False(t, ids[id], "two metered lanes must keep distinct technical intents")
		ids[id] = true
		require.Empty(t, journal.input.Evidence.UpstreamResponseID)
		for _, event := range writes {
			require.NotContains(t, string(event), id, "technical intent must never become a downstream response ID")
		}
	}
	require.Len(t, ids, 2)
}
