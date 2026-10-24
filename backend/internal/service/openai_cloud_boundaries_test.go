package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
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
	ended := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer conn.CloseNow()
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
			body, _ := json.Marshal(map[string]any{"model": "client-model", "input": "follow up", "instructions": "changed instructions", "previous_response_id": first.ResponseID})
			followCtx, err := svc.PrepareCloudContinuation(ctx, in.User.ID, in.APIKey.ID, *in.APIKey.GroupID, body)
			require.NoError(t, err)
			reservation := &InflightReservation{ttl: time.Minute, userID: in.User.ID, requestID: "reserved-followup"}
			reservation.refs.Store(1)
			defer reservation.Release()
			followCtx = WithInflightReservation(followCtx, reservation)
			followCtx, err = svc.CaptureCloudBillingSnapshot(followCtx, in)
			require.NoError(t, err)
			if cancelAtRejection {
				cancelOnReject.Store(true)
			}
			result, err := invoke(followCtx, body)
			require.Nil(t, result)
			require.ErrorContains(t, err, "generation settings changed")
			require.ErrorIs(t, err, ErrOpenAICloudNotStarted)
			require.Equal(t, 1, store.settlementCount())
			require.Empty(t, svc.cloudBillingWorker.holds)
			reservation.HandlerDone()
			require.Zero(t, reservation.refs.Load())

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

func TestCloudAbandonedToolWaitInterruptsAndRetainsUnknownUsage(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "pending", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}, `"tool-rpc"`))
	a.lastClientActivity = time.Now().Add(-2 * openAIWSPassthroughIdleTimeoutDefault)
	a.waitingSince = a.lastClientActivity
	events := make(chan cloudRPCMessage, 1)
	a.upstream = events
	done := make(chan struct{})
	go func() {
		defer close(done)
		interrupt := cloudTestMessage(t, a.ctx, wire)
		require.Equal(t, "turn/interrupt", interrupt.Method)
		cloudTestRead(wire, map[string]any{"id": interrupt.ID, "result": map[string]any{}})
		events <- cloudRPCMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}
	}()
	require.True(t, a.expireIdle(time.Now()))
	<-done
	require.Equal(t, "interrupted", a.record.Status)
	require.Len(t, store.settlements, 1)
	require.True(t, store.settlements[0].Unknown, "interruption does not establish final usage")
	for _, tool := range a.tools {
		require.False(t, tool.Submitted)
	}
}
