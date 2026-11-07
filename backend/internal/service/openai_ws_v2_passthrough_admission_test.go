package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPassthroughAdmissionWaitsForSettlementAndSendReturn(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	ctx := owner.context(t.Context())
	upstream := newStagedPassthroughConn()
	delayed := &latencyDelayedWriteConn{stagedPassthroughConn: upstream, release: make(chan struct{})}
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	svc.openaiWSPassthroughDialer = &stagedPassthroughDialer{conn: delayed}
	settling, settled := make(chan struct{}), make(chan struct{})
	var finishSettlement, finishSend sync.Once
	releaseSettlement := func() { finishSettlement.Do(func() { close(settled) }) }
	releaseSend := func() { finishSend.Do(func() { close(delayed.release) }) }
	server, serverErr := startPassthroughHookRecordingServer(t, ctx, svc, passthroughLifecycleAccount(), &OpenAIWSIngressHooks{AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
		if turn == 1 {
			close(settling)
			<-settled
		}
	}})
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer client.CloseNow()
	defer releaseSend()
	defer releaseSettlement()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(`{"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	select {
	case <-settling:
	case <-time.After(time.Second):
		t.Fatal("settlement did not start")
	}
	waitOpenAIWSAdmissions(t, owner, 1, 0, 1)
	releaseSettlement()
	readNativeWSTestTurn(t, client)
	waitOpenAIWSAdmissions(t, owner, 1, 1, 0)
	wctx, cancel := context.WithTimeout(ctx, time.Second)
	require.NoError(t, client.Write(wctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"next"}`)))
	cancel()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(`{"type":"response.completed","response":{"id":"r2","usage":{"input_tokens":2,"output_tokens":1}}}`)
	readNativeWSTestTurn(t, client)
	waitOpenAIWSAdmissions(t, owner, 2, 1, 1)
	releaseSend()
	waitOpenAIWSAdmissions(t, owner, 2, 2, 0)
	requireNativeWSTestServerExit(t, client, serverErr)
}

func TestPassthroughAdmissionWarmupAndDeferredBareError(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	ctx := owner.context(t.Context())
	upstream := newStagedPassthroughConn()
	server, serverErr := startPassthroughHookRecordingServer(t, ctx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), passthroughLifecycleAccount(), nil)
	defer server.Close()
	client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-5.1","generate":false,"input":"warm"}`)
	defer client.CloseNow()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(`{"type":"response.completed","response":{"status":"completed","output":[]}}`)
	readNativeWSTestTurn(t, client)
	waitOpenAIWSAdmissions(t, owner, 1, 1, 0)
	wctx, cancel := context.WithTimeout(ctx, time.Second)
	require.NoError(t, client.Write(wctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"next"}`)))
	cancel()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(`{"type":"error","error":{"code":"invalid_request_error","message":"rejected"}}`)
	_, err := readPassthroughLifecycleFrame(t, client, time.Second)
	require.NoError(t, err)
	waitOpenAIWSAdmissions(t, owner, 2, 1, 1)
	upstream.Send(`{"type":"response.failed","response":{"id":"failed","usage":{"input_tokens":8,"output_tokens":4}}}`)
	readNativeWSTestTurn(t, client)
	waitOpenAIWSAdmissions(t, owner, 2, 2, 0)
	requireNativeWSTestServerExit(t, client, serverErr)
}

func TestPassthroughAdmissionMultiplexQueueWaitsForUsageAndSend(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	ctx := owner.context(t.Context())
	upstream := newStagedPassthroughConn()
	delayed := &latencyDelayedWriteConn{stagedPassthroughConn: upstream, release: make(chan struct{})}
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
	svc := newPassthroughLifecycleService(cfg, upstream)
	svc.openaiWSPassthroughDialer = &stagedPassthroughDialer{conn: delayed}
	conn := newQueuedWSDownstreamTestConn()
	conn.writes = make(chan []byte, 16)
	settling, settled := make(chan struct{}), make(chan struct{})
	var usageOnce, sendOnce sync.Once
	releaseUsage := func() { usageOnce.Do(func() { close(settled) }) }
	releaseSend := func() { sendOnce.Do(func() { close(delayed.release) }) }
	done := make(chan error, 1)
	defer func() {
		releaseUsage()
		releaseSend()
		_ = conn.CloseNow()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("multiplexed relay did not join")
		}
	}()
	go func() {
		done <- RunOpenAIWSMultiplexer(ctx, conn, cfg, func(laneCtx context.Context, ds *OpenAIWSDownstream) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(laneCtx)
			_, first, err := ds.Read(laneCtx)
			if err != nil {
				return
			}
			_ = svc.ProxyResponsesWebSocketFromClient(laneCtx, c, ds, passthroughLifecycleAccount(), "sk-test", first, &OpenAIWSIngressHooks{
				AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
					if turn == 1 {
						close(settling)
						<-settled
					}
				},
			})
		})
		close(done)
	}()
	conn.input <- []byte("{\"type\":\"response.create\",\"stream_id\":\"a\",\"event_id\":\"one\",\"model\":\"gpt-5.1\",\"input\":\"one\"}")
	conn.input <- []byte("{\"type\":\"response.create\",\"stream_id\":\"a\",\"event_id\":\"two\",\"model\":\"gpt-5.1\",\"input\":\"two\"}")
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send("{\"type\":\"response.completed\",\"response\":{\"id\":\"one\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}")
	select {
	case <-settling:
	case <-time.After(time.Second):
		t.Fatal("first usage did not start")
	}
	waitOpenAIWSAdmissions(t, owner, 2, 0, 2)
	select {
	case <-upstream.writes:
		t.Fatal("queued create bypassed unfinished usage")
	default:
	}
	releaseUsage()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	waitOpenAIWSAdmissions(t, owner, 2, 1, 1)
	upstream.Send("{\"type\":\"response.completed\",\"response\":{\"id\":\"two\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}")
	receivedSecond := false
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for !receivedSecond {
		select {
		case event := <-conn.writes:
			receivedSecond = gjson.GetBytes(event, "type").String() == "response.completed" && gjson.GetBytes(event, "response.id").String() == "two"
		case <-deadline.C:
			t.Fatal("second terminal was not delivered")
		}
	}
	waitOpenAIWSAdmissions(t, owner, 2, 1, 1)
	releaseSend()
	waitOpenAIWSAdmissions(t, owner, 2, 2, 0)
}
