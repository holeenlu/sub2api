package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/billing/reservation"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloudCancelledPreparationDoesNotStartTurn(t *testing.T) {
	for _, phase := range []string{"connect", "thread"} {
		t.Run(phase, func(t *testing.T) {
			store := newCloudLiveRoutingStore()
			wire := newCloudRPCFakeConn()
			entered, proceed := make(chan struct{}), make(chan struct{})
			sessions := NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
				if phase == "connect" {
					close(entered)
					<-proceed
				}
				rpc := newCloudRPC(ctx, wire)
				rpc.mu.Lock()
				rpc.ready = true
				rpc.mu.Unlock()
				return rpc, nil
			}, func(context.Context, OpenAICloudOwner) error { return nil })
			defer sessions.Close()
			defer wire.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-test", SettingsDigest: "settings"}
			go func() {
				_, err := sessions.Run(ctx, OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"gpt-test","input":"hello"}`), RootRequestID: "root", BillingSnapshot: json.RawMessage(`{"frozen":true}`)}, nil)
				result <- err
			}()
			var thread cloudRPCMessage
			if phase == "thread" {
				thread = cloudTestMessage(t, t.Context(), wire)
				require.Equal(t, "thread/start", thread.Method)
			} else {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("connect not entered")
				}
			}
			cancel()
			require.ErrorIs(t, <-result, context.Canceled)
			if phase == "connect" {
				close(proceed)
			} else {
				cloudTestRead(wire, map[string]any{"id": thread.ID, "result": map[string]any{"model": "gpt-test", "thread": map[string]any{"id": "thread"}}})
			}
			require.Eventually(t, func() bool { return store.settlementCount() == 1 }, 2*time.Second, 10*time.Millisecond)
			bill := store.snapshot()
			require.NotNil(t, bill.Usage)
			require.Zero(t, bill.Usage.TotalTokens)
			require.Equal(t, "failed", bill.Status)
			select {
			case raw := <-wire.writes:
				require.Equal(t, "thread/archive", gjson.GetBytes(raw, "method").String())
				cloudTestRead(wire, map[string]any{"id": json.RawMessage(gjson.GetBytes(raw, "id").Raw), "result": map[string]any{}})
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

func TestCloudEmptyItemKeepsFirstOutputDeadline(t *testing.T) {
	svc, in, _, wire := cloudForwardFixture(t, nil)
	svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireStart(t, ctx, wire)
		wire.reads <- []byte(`{"method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"id":"answer","type":"agentMessage"}}}`)
		select {
		case <-time.After(1600 * time.Millisecond):
			cloudWireComplete(wire)
		case <-ctx.Done():
		}
	}()
	defer func() { cancel(); <-done }()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("api_key", in.APIKey)
	SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
	started := time.Now()
	result, err := svc.forwardOpenAICloud(ctx, c, in.Account, []byte(`{"model":"client-model","input":"hi","stream":false}`))
	require.ErrorIs(t, err, ErrOpenAIWSFirstOutputBudgetExhausted)
	require.Nil(t, result)
	require.Less(t, time.Since(started), 1500*time.Millisecond)

}

func TestCloudLargeTerminalAndWebSocketFraming(t *testing.T) {
	store := &cloudActorTestStore{refs: map[string]bool{}}
	actor := &cloudSessionActor{ctx: t.Context(), service: &OpenAICloudSessions{store: store}, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{SessionID: "s", RootRequestID: "r", Status: "completed"}}, requestID: "request", responseID: "resp_cloud_large", completed: map[string]*OpenAICloudResult{}, output: []map[string]any{{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": strings.Repeat("x", 8<<20)}}}}}
	require.NoError(t, actor.finish("completed", false, true, nil, nil))
	require.Greater(t, len(store.record.Result.Response), 4<<20)
	require.Equal(t, "completed", gjson.GetBytes(store.record.Result.Response, "status").String())
	// The event wrapper may be larger than the result JSON itself. Cloud uses
	// the envelope budget, keeping a near-limit terminal frame deliverable.
	payload, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]string{"text": strings.Repeat("x", OpenAICloudMaxResultBytes-32)}})
	require.NoError(t, err)
	emitted := false
	writer := &responsesEventWriter{header: http.Header{"Content-Type": {"text/event-stream"}}, size: -1, maxFrameBytes: OpenAICloudMaxEnvelopeBytes, write: func(raw []byte) error { emitted = json.Valid(raw); return nil }}
	err = writer.WriteResponseEvent(payload)
	require.NoError(t, err)
	require.True(t, emitted)
}

func TestCloudOversizedTerminalPreservesUsage(t *testing.T) {
	store := &cloudActorTestStore{refs: map[string]bool{}}
	usage := &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
	actor := &cloudSessionActor{ctx: t.Context(), service: &OpenAICloudSessions{store: store}, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{SessionID: "s", RootRequestID: "r", Status: "completed"}, Usage: usage}, requestID: "request", responseID: "resp_cloud_result", completed: map[string]*OpenAICloudResult{}, output: []map[string]any{{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": strings.Repeat("<", OpenAICloudMaxResultBytes/5)}}}}}
	require.NoError(t, actor.finish("completed", false, true, usage, nil))
	require.Len(t, store.settlements, 1)
	require.False(t, store.settlements[0].Unknown)
	require.Equal(t, usage, store.settlements[0].Usage)
	require.Equal(t, 502, store.record.Result.ErrorStatus)
	require.True(t, store.record.Result.UsageCredible)
	require.Equal(t, "cloud_response_too_large", gjson.GetBytes(store.record.Result.Response, "error.code").String())
	require.Less(t, len(store.record.Result.Response), 1024)
}

func TestCloudWarmupContinuationRestoresLocalHistory(t *testing.T) {
	svc, in, _, wire := cloudForwardFixture(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	admission := &openAIWSTestAdmission{}
	ctx = admission.context(ctx)
	ended := make(chan error, 1)
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			ended <- err
			return
		}
		defer conn.CloseNow()
		_, first, err := conn.Read(ctx)
		if err != nil {
			ended <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		c.Set("api_key", in.APIKey)
		hooks := &OpenAIWSIngressHooks{PrepareCloudTurn: func(c context.Context, _ int, _ []byte, _ string) (context.Context, error) {
			return svc.CaptureCloudBillingSnapshot(c, in)
		}}
		ended <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, in.Account, "unused", first, hooks)
	}))
	defer server.Close()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { cancel(); _ = conn.CloseNow(); <-serverDone }()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"client-model","generate":false,"input":"warmup history"}`)))
	id := ""
	for id == "" {
		_, raw, err := conn.Read(ctx)
		require.NoError(t, err)
		if gjson.GetBytes(raw, "type").String() == "response.completed" {
			id = gjson.GetBytes(raw, "response.id").String()
		}
	}
	require.True(t, strings.HasPrefix(id, "resp_bridge_warmup_"))
	follow, _ := json.Marshal(map[string]any{"type": "response.create", "model": "client-model", "previous_response_id": id, "input": "continue"})
	upstreamDone := make(chan struct{})
	go func() {
		defer close(upstreamDone)
		cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
		require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
		cloudWireResponse(t, ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		injection := cloudTestMessage(t, ctx, wire)
		require.Equal(t, "thread/inject_items", injection.Method)
		require.Contains(t, string(injection.Params), "warmup history")
		cloudTestRead(wire, map[string]any{"id": injection.ID, "result": map[string]any{}})
		cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		cloudWireComplete(wire)
	}()
	defer func() { cancel(); <-upstreamDone }()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, follow))
	for {
		_, raw, err := conn.Read(ctx)
		require.NoError(t, err)
		if gjson.GetBytes(raw, "type").String() == "response.completed" {
			require.Contains(t, string(raw), "resp_cloud_")
			break
		}
	}
	_ = conn.CloseNow()
	cancel()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("WS did not stop")
	}
	waitOpenAIWSAdmissions(t, admission, 2, 2, 0)

}

func TestCloudFailedManualTestDoesNotChangeAuthorizationOrActivation(t *testing.T) {
	// The fixture has no usage HTTP client, so the manual probe fails after
	// model admission without contacting a real upstream or changing settings.
	svc, in, _, _ := cloudForwardFixture(t, nil)
	repo := svc.cloudService.repo.(*cloudAuthorizationRepoFixture)
	before := cloudLiveClone(repo.binding)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := authz.WithSubject(t.Context(), authz.Subject{UserID: 7, Role: authz.SuperAdmin, ManagementRequest: true})
	c.Request = httptest.NewRequest("POST", "/api/v1/admin/accounts/1/test", nil).WithContext(ctx)
	tests := &AccountTestService{openaiGatewayService: svc}
	require.Error(t, tests.testOpenAICloudConnection(c, in.Account, "client-model", ""))
	require.Contains(t, recorder.Body.String(), "cloud_upstream_error", "the allowed alias must reach probe execution")
	require.Equal(t, before, repo.binding)
	require.Zero(t, repo.writes)
	require.True(t, in.Account.IsOpenAICloudEnabled(), "failed manual testing cannot change the account switch")
}

type cloudInflightReleaseProbe struct {
	released atomic.Int32
}

func (c *cloudInflightReleaseProbe) ReleaseInflightBalance(context.Context, int64, string) error {
	c.released.Add(1)
	return nil
}

func TestCloudRejectedContinuationReleasesUnstartedBillingHold(t *testing.T) {
	for _, cancelAtRejection := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelAtRejection), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var cancelOnReject atomic.Bool
			svc.cloudSessions.store = &cloudRejectCancelStore{OpenAICloudSessionStore: store, cancel: func() {
				if cancelOnReject.Load() {
					cancel()
				}
			}}

			done := make(chan struct{})
			go func() { defer close(done); cloudWireStart(t, ctx, wire); cloudWireComplete(wire) }()
			defer func() { cancel(); <-done }()
			invoke := func(requestCtx context.Context, body []byte) (*OpenAIForwardResult, error) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				c.Set("api_key", in.APIKey)
				SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
				return svc.forwardOpenAICloud(requestCtx, c, in.Account, body)
			}
			firstCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
			require.NoError(t, err)
			first, err := invoke(firstCtx, []byte(`{"model":"client-model","input":"hi","stream":false}`))
			require.NoError(t, err)
			body, _ := json.Marshal(map[string]any{"model": "client-model", "input": []any{map[string]any{"role": "assistant", "content": "fabricated earlier history"}, cloudUserMessage("follow up")}, "previous_response_id": first.ResponseID})
			followCtx, err := svc.PrepareCloudContinuation(ctx, in.User.ID, in.APIKey.ID, *in.APIKey.GroupID, body)
			require.NoError(t, err)
			cache := &cloudInflightReleaseProbe{}
			reservation := reservation.New(reservation.Options{TTL: time.Minute, UserID: in.User.ID, RequestID: "reserved-followup", Balance: cache})
			defer reservation.Release()
			followCtx = WithInflightReservation(followCtx, reservation)
			followCtx, err = svc.CaptureCloudBillingSnapshot(followCtx, in)
			require.NoError(t, err)
			if cancelAtRejection {
				cancelOnReject.Store(true)
			}
			result, err := invoke(followCtx, body)
			require.Nil(t, result)
			require.ErrorContains(t, err, "existing history")
			require.ErrorIs(t, err, ErrOpenAICloudNotStarted)
			require.Equal(t, 1, store.settlementCount())
			require.Empty(t, svc.cloudBillingWorker.holds)
			reservation.HandlerDone()
			require.Equal(t, int32(1), cache.released.Load())

		})
	}
}

func TestCloudExpiredCommandCannotDispatch(t *testing.T) {
	actor := &cloudSessionActor{ctx: t.Context(), service: &OpenAICloudSessions{store: &cloudActorTestStore{}}}
	require.ErrorIs(t, actor.handleCommand(OpenAICloudCommand{Deadline: time.Now().Add(-time.Second), RequestID: "expired"}), context.DeadlineExceeded)
}

// Cancel immediately before returning the owner's non-dispatch proof.
type cloudRejectCancelStore struct {
	OpenAICloudSessionStore
	cancel context.CancelFunc
}

func (s *cloudRejectCancelStore) ReadCloudEvents(ctx context.Context, id, cursor string) ([]OpenAICloudStreamMessage, error) {
	messages, err := s.OpenAICloudSessionStore.ReadCloudEvents(ctx, id, cursor)
	for _, m := range messages {
		var e OpenAICloudStreamEvent
		if json.Unmarshal(m.Payload, &e) == nil && e.NotStarted {
			s.cancel()
		}
	}
	return messages, err
}

func TestCloudSessionExpiryTracksIdleStateInsteadOfCreationTime(t *testing.T) {
	now := time.Now()
	a := &cloudSessionActor{service: &OpenAICloudSessions{}, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{Status: "running", ExpiresAt: now.Add(-time.Hour)}}}
	a.idleSince.Store(now.Add(-time.Hour).UnixNano())
	require.False(t, a.completedIdleExpired(now), "an active turn outlives the original retention timestamp")
	a.record.Status = "completed"
	require.True(t, a.completedIdleExpired(now))
	a.idleSince.Store(now.Add(-time.Minute).UnixNano())
	require.False(t, a.completedIdleExpired(now), "idle starts at completion rather than thread creation")
	a.record.Status = "awaiting_tools"
	a.lastClientActivity = now.Add(-20 * time.Minute)
	require.False(t, a.completedIdleExpired(now))
	require.False(t, a.toolWaitExpired(now), "a long client tool is not interrupted at 15 minutes")
	a.lastClientActivity = now.Add(-openAIWSPassthroughIdleTimeoutDefault)
	require.True(t, a.toolWaitExpired(now), "abandoned tools still have the existing WS idle bound")
	a.waitingSince = now.Add(-time.Minute)
	require.False(t, a.toolWaitExpired(now), "model runtime before the handoff is not client tool idle time")
	a.lastClientActivity = now
	require.False(t, a.toolWaitExpired(now))
}

// The abandoned wait is interrupted and the upstream confirms the interruption,
// so the usage measured before the handoff is settled as a lower bound.
func TestCloudAbandonedToolWaitInterruptsAndSettlesLowerBound(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "pending", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}, `"tool-rpc"`))
	a.lastClientActivity = time.Now().Add(-2 * openAIWSPassthroughIdleTimeoutDefault)
	a.waitingSince = a.lastClientActivity
	events := make(chan cloudRPCDelivery, 1)
	a.upstream = events
	done := make(chan struct{})
	go func() {
		defer close(done)
		interrupt := cloudTestMessage(t, a.ctx, wire)
		require.Equal(t, "turn/interrupt", interrupt.Method)
		cloudTestRead(wire, map[string]any{"id": interrupt.ID, "result": map[string]any{}})
		events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}}
	}()
	require.True(t, a.expireIdle(time.Now()))
	<-done
	require.Equal(t, "interrupted", a.record.Status)
	require.Len(t, store.settlements, 1)
	require.False(t, store.settlements[0].Unknown, "a confirmed interruption bills the usage measured so far")
	require.True(t, store.settlements[0].LowerBound)
	require.Equal(t, 12, store.settlements[0].Usage.TotalTokens)
	require.Len(t, a.tools, 1, "the abandoned call was never answered")
}

// Confirmed interruption keeps the cause that initiated it, both with an
// attached response and in the gap after handing tools to the client.
func TestCloudLocalStopReasonSurvivesConfirmedInterruption(t *testing.T) {
	for _, scenario := range []struct {
		name, message, reason string
		upstreamError         bool
	}{
		{"client cancellation", "Cloud client cancelled the request", "client_cancelled", false},
		{"authorization change", "Cloud authorization or account settings changed", "authorization_changed", false},
		{"authority check unavailable", errCloudStateUnavailable.Error(), "authority_check_unavailable", false},
		{"service shutdown", "Cloud session expired or service stopped", "service_stopping", false},
		{"interruption with upstream error", "Cloud client cancelled the request", "client_cancelled", true},
	} {
		for _, waiting := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tool_wait_%t", scenario.name, waiting), func(t *testing.T) {
				a, store, wire := cloudHostedActorForTest(t)
				if waiting {
					require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "pending", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}, `"tool-rpc"`))
					require.Empty(t, a.requestID)
				}
				events := make(chan cloudRPCDelivery, 1)
				a.upstream = events
				script := runCloudScript(func() error {
					if _, err := cloudExpectReply(a.ctx, wire, "turn/interrupt", map[string]any{}); err != nil {
						return err
					}
					turn := map[string]any{"id": "turn", "status": "interrupted"}
					if scenario.upstreamError {
						turn["error"] = map[string]any{"code": "rate_limit_exceeded", "message": "private provider detail"}
					}
					params, err := json.Marshal(map[string]any{"threadId": "thread", "turn": turn})
					if err != nil {
						return err
					}
					events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "turn/completed", Params: params}}
					return nil
				})
				a.stop(scenario.message)
				script.join(t)
				require.Equal(t, "interrupted", a.record.Status)
				require.Len(t, store.settlements, 1)
				require.False(t, store.settlements[0].Unknown)
				require.True(t, store.settlements[0].LowerBound)
				require.Equal(t, 12, store.settlements[0].Usage.TotalTokens)
				require.Equal(t, scenario.reason, a.publicFailure["gateway_stop_reason"])
				if scenario.upstreamError {
					require.Equal(t, "rate_limit_exceeded", a.publicFailure["code"], "local stop cannot replace a classified provider failure")
				}
				if waiting {
					require.Len(t, a.tools, 1, "stopping must not answer or replay the pending tool")
				} else {
					require.NotNil(t, store.record.Result)
					require.Equal(t, scenario.reason, gjson.GetBytes(store.record.Result.Response, "error.gateway_stop_reason").String())
				}
				require.Empty(t, wire.writes)
			})
		}
	}
}

func TestCloudToolWaitBuffersByBytesNotEventCount(t *testing.T) {
	actor := &cloudSessionActor{record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{ThreadID: "thread", TurnID: "turn", Status: "awaiting_tools"}}}
	params, err := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "text", "delta": "a"})
	require.NoError(t, err)
	for range 1024 {
		require.NoError(t, actor.handleEvent(cloudRPCMessage{Method: "item/agentMessage/delta", Params: params}), "small chunks should share the byte budget, not an independent event-count gate")
	}
	require.Len(t, actor.bufferedEvents, 1024)
	oversized, err := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "text", "delta": strings.Repeat("x", OpenAICloudMaxResultBytes)})
	require.NoError(t, err)
	require.Error(t, actor.handleEvent(cloudRPCMessage{Method: "item/agentMessage/delta", Params: oversized}), "the memory budget remains enforced")
	require.Len(t, actor.bufferedEvents, 1024)
}

func TestCloudPreparationReceivesRequestDeadlineAndCancellation(t *testing.T) {
	for _, mode := range []string{"cancel", "remote_cancel", "deadline", "actor_cancel"} {
		t.Run(mode, func(t *testing.T) {
			store := newCloudLiveRoutingStore()
			entered, stopped := make(chan context.Context, 1), make(chan error, 1)
			force := make(chan struct{})
			defer close(force)
			sessions := NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
				entered <- ctx
				select {
				case <-ctx.Done():
					stopped <- ctx.Err()
					return nil, ctx.Err()
				case <-force:
					return nil, fmt.Errorf("test cleanup")
				}
			}, func(context.Context, OpenAICloudOwner) error { return nil })
			defer sessions.Close()
			timeout := 15 * time.Second
			if mode == "deadline" {
				timeout = time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			expected, _ := ctx.Deadline()
			owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-test", SettingsDigest: "settings"}
			body, _ := json.Marshal(map[string]any{"model": "gpt-test", "input": "hello"})
			type runOutcome struct {
				result *OpenAICloudResult
				err    error
			}
			result := make(chan runOutcome, 1)
			runDone := make(chan struct{})
			go func() {
				defer close(runDone)
				response, err := sessions.Run(ctx, OpenAICloudRequest{Owner: owner, Body: body, RootRequestID: "root", BillingSnapshot: json.RawMessage("{}")}, nil)
				result <- runOutcome{response, err}
			}()
			defer func() { cancel(); <-runDone }()
			var operation context.Context
			select {
			case operation = <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("preparation did not start")
			}
			actor := sessions.localActor(store.snapshot().SessionID)
			require.NotNil(t, actor)
			switch mode {
			case "deadline":
				actual, ok := operation.Deadline()
				require.True(t, ok, "preparation must receive the originating request deadline")
				require.Equal(t, expected, actual)
			case "remote_cancel":
				record := store.snapshot()
				actor := sessions.localActor(record.SessionID)
				pending := actor.preparation.Load()
				require.NotNil(t, pending)
				require.NoError(t, store.DeleteCloudEvents(t.Context(), pending.requestID))
				require.NoError(t, store.EnqueueCloudCommand(t.Context(), record.SessionID, OpenAICloudCommand{
					RequestID: pending.requestID, Request: OpenAICloudRequest{Owner: owner}, Cancel: true,
				}))
			case "actor_cancel":
				// Cancel only this actor. Manager shutdown would cancel Run's
				// caller too, hiding the terminal-response contract under test.
				actor.cancel()
			default:
				cancel()
			}
			select {
			case err := <-stopped:
				require.Error(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled/deadline-expired preparation kept running")
			}
			if mode != "actor_cancel" {
				cancel()
			}
			select {
			case outcome := <-result:
				if mode == "actor_cancel" {
					// The caller is still connected when the actor shuts down, so
					// it receives a failed terminal response rather than a call error.
					require.NoError(t, outcome.err)
					require.NotNil(t, outcome.result)
					require.True(t, outcome.result.Terminal)
					require.False(t, outcome.result.AwaitingTools)
					require.Equal(t, "failed", gjson.GetBytes(outcome.result.Response, "status").String())
					require.GreaterOrEqual(t, outcome.result.ErrorStatus, http.StatusBadRequest)
				} else {
					require.Error(t, outcome.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled preparation did not deliver its outcome")
			}
			require.Eventually(t, func() bool { return store.settlementCount() == 1 }, time.Second, time.Millisecond)
			record := store.snapshot()
			require.Equal(t, "failed", record.Status, "preparation cancellation must never become completed")
			require.NotNil(t, record.Usage)
			require.Zero(t, record.Usage.TotalTokens)
			bill := store.snapshotSettlement("root")
			require.NotNil(t, bill)
			require.Equal(t, owner, bill.OpenAICloudOwner)
			require.False(t, bill.Unknown, "preparation never sent a generation")
			select {
			case <-actor.done:
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled actor did not join its children")
			}
			if mode == "actor_cancel" {
				require.NoError(t, ctx.Err(), "the caller remains live after actor cancellation")
				next, finish, err := sessions.beginOperation(t.Context())
				require.NoError(t, err, "actor cancellation must leave manager admission open")
				defer finish()
				require.NoError(t, next.Err())
			}
		})
	}
}

func TestCloudPreparationCancelledBeforeConnectionDoesNotDial(t *testing.T) {
	store := newCloudLiveRoutingStore()
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, AccountID: 4, Model: "gpt-test"}
	command := OpenAICloudCommand{RequestID: "closed-before-prepare", Request: OpenAICloudRequest{Owner: owner}}
	require.NoError(t, store.DeleteCloudEvents(t.Context(), command.RequestID))
	sessions := NewOpenAICloudSessions(store, func(context.Context, OpenAICloudOwner) (*cloudRPC, error) {
		t.Fatal("a cancelled command must never connect")
		return nil, nil
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	defer sessions.Close()
	actor := &cloudSessionActor{service: sessions, ctx: sessions.ctx}
	_, err := actor.prepareConnection(command, sessions.ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, actor.preparation.Load())
}

func TestCloudActiveReadDeadlinePreservesInactiveSessions(t *testing.T) {
	now := time.Now()
	actor := &cloudSessionActor{requestID: "active", rpc: &cloudRPC{readTimeout: 37 * time.Second},
		record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{Status: "running"}}}
	actor.rpc.lastReadAt.Store(now.UnixNano())
	require.Equal(t, now.Add(37*time.Second).UnixNano(), actor.activeReadDeadline().UnixNano())
	actor.rpc.lastReadAt.Store(now.Add(time.Second).UnixNano())
	require.Equal(t, now.Add(38*time.Second).UnixNano(), actor.activeReadDeadline().UnixNano(), "each upstream message renews native read inactivity")
	actor.lastClientActivity = now.Add(10 * time.Minute)
	require.Equal(t, actor.lastClientActivity.Add(37*time.Second), actor.activeReadDeadline(), "a resumed tool starts a fresh wait")
	actor.requestID = ""
	actor.record.Status = "awaiting_tools"
	require.True(t, actor.activeReadDeadline().IsZero(), "handed-off tools use client idle policy")
	actor.record.Status = "completed"
	require.True(t, actor.activeReadDeadline().IsZero(), "completed thread retention is unchanged")
}

func TestCloudActiveTurnReadIdleUsesNativeTimeout(t *testing.T) {
	store := newCloudLiveRoutingStore()
	wire := newCloudRPCFakeConn()
	gateway := &OpenAIGatewayService{cfg: newOpenAIWSExecutionScopeTestConfig()}
	gateway.cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 1
	sessions := NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
		rpc := newCloudRPC(withCloudRPCTimeouts(ctx, gateway), wire)
		if err := rpc.Initialize(ctx); err != nil {
			return nil, err
		}
		return rpc, nil
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	defer sessions.Close()
	defer wire.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireStart(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "answer", "type": "agentMessage"}}})
		cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "answer", "delta": "partial"}})
		message := cloudTestMessage(t, ctx, wire)
		require.Equal(t, "turn/interrupt", message.Method, "a post-output stalled turn must not wait forever")
		cloudTestRead(wire, map[string]any{"id": message.ID, "result": map[string]any{}})
		cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}}})
	}()
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}
	body, _ := json.Marshal(map[string]any{"model": "gpt-5.1", "input": "hello"})
	result, err := sessions.Run(ctx, OpenAICloudRequest{Owner: owner, Body: body, RootRequestID: "read-timeout", BillingSnapshot: json.RawMessage("{}")}, nil)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	require.Equal(t, http.StatusGatewayTimeout, result.ErrorStatus)
	require.Equal(t, "cloud_upstream_timeout", gjson.GetBytes(result.Response, "error.code").String())
	<-done
	require.Equal(t, 1, store.settlementCount())
}

type cloudUnavailableRequestStateStore struct{ OpenAICloudSessionStore }

func (s *cloudUnavailableRequestStateStore) CloudRequestClosed(context.Context, string) (bool, error) {
	return false, fmt.Errorf("private Redis failure")
}
func TestCloudRequestStateFailureRemainsDependencyError(t *testing.T) {
	store := &cloudUnavailableRequestStateStore{OpenAICloudSessionStore: newCloudLiveRoutingStore()}
	var connects atomic.Int32
	sessions := NewOpenAICloudSessions(store, func(context.Context, OpenAICloudOwner) (*cloudRPC, error) {
		connects.Add(1)
		return nil, fmt.Errorf("must not connect")
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	defer sessions.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, AccountID: 3, GrantGeneration: "grant", SettingsDigest: "settings", Model: "gpt-test"}
	body, _ := json.Marshal(map[string]any{"model": "gpt-test", "input": "hello"})
	result, err := sessions.Run(ctx, OpenAICloudRequest{Owner: owner, RootRequestID: "root", BillingSnapshot: json.RawMessage("{}"), Body: body}, nil)
	require.Nil(t, result)
	status, code, message, known := cloudPublicError(err)
	require.True(t, known)
	require.Equal(t, 503, status)
	require.Equal(t, "cloud_state_unavailable", code)
	require.NotContains(t, message, "private")
	require.Zero(t, connects.Load())
}
