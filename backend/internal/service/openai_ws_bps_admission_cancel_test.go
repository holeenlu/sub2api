package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newBPSAdmissionDownstream(t *testing.T) (context.Context, *OpenAIWSDownstream, *queuedWSDownstreamTestConn, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(openAIWSTestAdmissionContext(t.Context()), 5*time.Second)
	t.Cleanup(cancel)
	conn := newQueuedWSDownstreamTestConn()
	conn.writes = make(chan []byte, 16)
	ds := NewOpenAIWSDownstream(conn, 4096, ctx)
	t.Cleanup(func() { _ = ds.CloseNow(); ds.finishAdmissions() })
	conn.input <- []byte(`{"type":"response.create","stream_id":"a","event_id":"first","model":"gpt-5.1","input":"first"}`)
	_, payload, err := ds.Read(ctx)
	require.NoError(t, err)
	require.NoError(t, ds.bindRequestEnvelope(payload))
	return ctx, ds, conn, payload
}

func readBPSAdmissionEvent(t *testing.T, ctx context.Context, conn *queuedWSDownstreamTestConn) []byte {
	t.Helper()
	select {
	case event := <-conn.writes:
		return event
	case <-ctx.Done():
		t.Fatal("admission control event was not delivered")
		return nil
	}
}

func TestOpenAIWSBPSAdmissionControlOwnership(t *testing.T) {
	for _, target := range []string{"untargeted", "active_id", "other_id", "other_lane", "old_turn", "interrupt"} {
		t.Run(target, func(t *testing.T) {
			ctx, ds, conn, _ := newBPSAdmissionDownstream(t)
			current := "active"
			ds.currentResponseID.Store(&current)
			entered, proceed := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- ds.RunBPSAccountAdmission(ctx, func(waitCtx context.Context) error {
					close(entered)
					select {
					case <-waitCtx.Done():
						return waitCtx.Err()
					case <-proceed:
						return nil
					}
				})
			}()
			<-entered
			payload := `{"type":"response.cancel","stream_id":"a","event_id":"control"}`
			switch target {
			case "active_id":
				payload = `{"type":"response.cancel","stream_id":"a","response_id":"active","event_id":"control"}`
			case "other_id":
				payload = `{"type":"response.cancel","stream_id":"a","response_id":"old","event_id":"control"}`
			case "other_lane":
				payload = `{"type":"response.cancel","stream_id":"b","event_id":"control"}`
			case "interrupt":
				payload = `{"type":"response.interrupt","stream_id":"a","response_id":"active","event_id":"control"}`
			}
			if target == "old_turn" {
				// Model a previously queued control with the reader's old stamp.
				ds.controls <- openAIWSControl{payload: []byte(payload), turn: ds.turn.Load() - 1}
			} else {
				conn.input <- []byte(payload)
			}
			event := readBPSAdmissionEvent(t, ctx, conn)
			require.Equal(t, "control", gjson.GetBytes(event, "event_id").String())
			canceled := target == "untargeted" || target == "active_id"
			if canceled {
				require.Equal(t, "request_cancelled", gjson.GetBytes(event, "error.code").String())
			} else {
				code := "response_not_active"
				if target == "interrupt" {
					code = "unsupported_operation"
				}
				require.Equal(t, code, gjson.GetBytes(event, "error.code").String())
				require.True(t, ds.active.Load(), "mismatched controls must leave the current turn active")
				close(proceed)
			}
			select {
			case err := <-done:
				if canceled {
					require.ErrorIs(t, err, ErrOpenAIWSAdmissionCancelled)
					require.Equal(t, ds.turn.Load(), ds.terminalTurn.Load())
				} else {
					require.NoError(t, err)
				}
			case <-ctx.Done():
				t.Fatal("admission consumer did not join")
			}
			require.Empty(t, conn.writes, "one control must not create two terminal events")
		})
	}
}

func TestOpenAIWSBPSAdmissionAcquireCancelRace(t *testing.T) {
	ctx, ds, conn, _ := newBPSAdmissionDownstream(t)
	conn.input <- []byte(`{"type":"response.cancel","stream_id":"a","event_id":"stop"}`)
	require.Eventually(t, func() bool { return len(ds.controls) == 1 }, time.Second, time.Millisecond)
	// Even if acquire completes immediately and wins select, the already
	// observed cancellation must win before admission is handed to dispatch.
	called := false
	err := ds.RunBPSAccountAdmission(ctx, func(context.Context) error { called = true; return nil })
	require.True(t, called)
	require.ErrorIs(t, err, ErrOpenAIWSAdmissionCancelled)
	require.Equal(t, "request_cancelled", gjson.GetBytes(readBPSAdmissionEvent(t, ctx, conn), "error.code").String())
	require.Empty(t, conn.writes)
}

func TestOpenAIWSBPSAdmissionDisconnectStopsWait(t *testing.T) {
	ctx, ds, conn, _ := newBPSAdmissionDownstream(t)
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- ds.RunBPSAccountAdmission(ctx, func(waitCtx context.Context) error {
			close(entered)
			<-waitCtx.Done()
			return waitCtx.Err()
		})
	}()
	<-entered
	require.NoError(t, conn.CloseNow())
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, errors.Is(err, ErrOpenAIWSAdmissionCancelled))
	case <-ctx.Done():
		t.Fatal("disconnect did not stop admission")
	}
}

func TestOpenAIWSBPSBeforeTurnCancelNeverStartsBridge(t *testing.T) {
	ctx, ds, conn, payload := newBPSAdmissionDownstream(t)
	upstream := &httpUpstreamRecorder{}
	svc := excelBPSGatewayTestService(upstream)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
	entered, proceed := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- svc.ProxyResponsesWebSocketFromClient(ctx, c, ds, excelAccount(), "test-token", payload, &OpenAIWSIngressHooks{
			BeforeTurn: func(int) error {
				close(entered)
				select {
				case <-proceed:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("ingress failed before BeforeTurn: %v", err)
	case <-ctx.Done():
		t.Fatal("ingress did not reach BeforeTurn")
	}
	conn.input <- []byte(`{"type":"response.cancel","stream_id":"a","event_id":"stop"}`)
	require.Eventually(t, func() bool { return len(ds.controls) == 1 }, time.Second, time.Millisecond)
	close(proceed)
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrOpenAIWSAdmissionCancelled)
	case <-ctx.Done():
		t.Fatal("bridge did not observe the queued cancel")
	}
	require.Empty(t, upstream.requests, "cancel queued by the sole reader must prevent any BPS HTTP send")
	require.Equal(t, "request_cancelled", gjson.GetBytes(readBPSAdmissionEvent(t, ctx, conn), "error.code").String())
	require.Empty(t, conn.writes, "one terminal, no appended error/JSON")
}

func TestOpenAIWSBPSBeforeTurnCancelWithChainedMappingNeverStartsBridge(t *testing.T) {
	ctx, ds, conn, payload := newBPSAdmissionDownstream(t)
	upstream := &httpUpstreamRecorder{}
	svc := excelBPSGatewayTestService(upstream)
	account := excelAccount()
	account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-6-astra", "gpt-6-astra": "unselected"}
	account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
	entered, proceed := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- svc.ProxyResponsesWebSocketFromClient(ctx, c, ds, account, "test-token", payload, &OpenAIWSIngressHooks{
			BeforeTurn: func(int) error {
				close(entered)
				select {
				case <-proceed:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("ingress failed before BeforeTurn: %v", err)
	case <-ctx.Done():
		t.Fatal("ingress did not reach BeforeTurn")
	}
	conn.input <- []byte(`{"type":"response.cancel","stream_id":"a","event_id":"stop"}`)
	require.Eventually(t, func() bool { return len(ds.controls) == 1 }, time.Second, time.Millisecond)
	close(proceed)
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrOpenAIWSAdmissionCancelled)
	case <-ctx.Done():
		t.Fatal("bridge did not observe the queued cancel")
	}
	require.Empty(t, upstream.requests, "cancel queued by the sole reader must prevent any BPS HTTP send")
	require.Equal(t, "request_cancelled", gjson.GetBytes(readBPSAdmissionEvent(t, ctx, conn), "error.code").String())
	require.Empty(t, conn.writes, "one terminal, no appended error/JSON")
}
