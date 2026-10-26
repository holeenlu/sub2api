//go:build unit

package service

import (
	"context"
	"encoding/json"
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

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type multiplexObserved struct {
	stream, event string
	payload       []byte
	ds            *OpenAIWSDownstream
}

func multiplexFrame(stream, event, parent string) []byte {
	body := map[string]any{"type": "response.create", "model": "gpt-6-luna", "stream_id": stream, "event_id": event, "input": "hello"}
	if parent != "" {
		body["previous_response_id"] = parent
	}
	raw, _ := json.Marshal(body)
	return raw
}
func startMultiplexFixture(t *testing.T, serve func(context.Context, *OpenAIWSDownstream)) *queuedWSDownstreamTestConn {
	t.Helper()
	conn := newQueuedWSDownstreamTestConn()
	conn.input = make(chan []byte, 128)
	conn.writes = make(chan []byte, 128)
	done := make(chan error, 1)
	go func() { done <- RunOpenAIWSMultiplexer(context.Background(), conn, nil, serve) }()
	t.Cleanup(func() {
		_ = conn.CloseNow()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("multiplexer workers leaked")
		}
	})
	return conn
}
func nextMultiplexObservation(t *testing.T, ch <-chan multiplexObserved) multiplexObserved {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
		return multiplexObserved{}
	}
}
func TestOpenAIWSMultiplexFIFOAndIndependentFailure(t *testing.T) {
	started := make(chan multiplexObserved, 8)
	finish := make(chan string, 8)
	var gates sync.Map
	for _, event := range []string{"a1", "a2", "b1"} {
		gates.Store(event, make(chan struct{}))
	}
	conn := startMultiplexFixture(t, func(ctx context.Context, ds *OpenAIWSDownstream) {
		for {
			_, payload, err := ds.Read(ctx)
			if err != nil {
				return
			}
			if ds.bindRequestEnvelope(payload) != nil {
				return
			}
			stream, event, _ := openAIWSRequestEnvelope(payload)
			started <- multiplexObserved{stream, event, payload, ds}
			gate, _ := gates.Load(event)
			select {
			case <-gate.(chan struct{}):
			case <-ds.done:
				return
			}
			if event == "b1" {
				_ = ds.Close(coderws.StatusPolicyViolation, "bad request on b")
				return
			}
			body, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_" + event, "output": []any{}}})
			if ds.Write(ctx, coderws.MessageText, body) != nil {
				return
			}
			ds.completeMultiplexTurn()
			finish <- event
		}
	})
	conn.input <- multiplexFrame("a", "a1", "")
	a := nextMultiplexObservation(t, started)
	require.Equal(t, "a1", a.event)
	conn.input <- multiplexFrame("a", "a2", "")
	conn.input <- multiplexFrame("b", "b1", "")
	b := nextMultiplexObservation(t, started)
	require.Equal(t, "b1", b.event)
	select {
	case got := <-started:
		t.Fatalf("same stream overlapped: %s", got.event)
	default:
	}
	gate, _ := gates.Load("b1")
	close(gate.(chan struct{}))
	select {
	case event := <-conn.writes:
		require.Equal(t, "b", gjson.GetBytes(event, "stream_id").String())
	case <-time.After(time.Second):
		t.Fatal("missing local error")
	}
	gate, _ = gates.Load("a1")
	close(gate.(chan struct{}))
	require.Equal(t, "a2", nextMultiplexObservation(t, started).event)
	gate, _ = gates.Load("a2")
	close(gate.(chan struct{}))
	for i := 0; i < 2; i++ {
		select {
		case <-finish:
		case <-time.After(time.Second):
			t.Fatal("healthy lane stopped")
		}
	}
}
func TestOpenAIWSMultiplexActiveAndNamedLimits(t *testing.T) {
	started := make(chan multiplexObserved, 40)
	conn := startMultiplexFixture(t, func(ctx context.Context, ds *OpenAIWSDownstream) {
		_, payload, err := ReadOpenAIWSClientMessage(ctx, ds, 50*time.Millisecond, coderws.StatusPolicyViolation, "client read timeout")
		if err != nil {
			return
		}
		stream, event, _ := openAIWSRequestEnvelope(payload)
		started <- multiplexObserved{stream, event, payload, ds}
		<-ds.done
	})
	for i := 0; i < 33; i++ {
		conn.input <- multiplexFrame(fmt.Sprintf("s%d", i), fmt.Sprint(i), "")
	}
	owners := make([]multiplexObserved, 0, 16)
	for i := 0; i < 16; i++ {
		owners = append(owners, nextMultiplexObservation(t, started))
	}
	select {
	case <-started:
		t.Fatal("more than 16 active turns")
	case <-time.After(75 * time.Millisecond):
	}
	select {
	case event := <-conn.writes:
		require.Equal(t, "stream_limit_exceeded", gjson.GetBytes(event, "error.code").String())
		require.Equal(t, "s32", gjson.GetBytes(event, "stream_id").String())
	case <-time.After(time.Second):
		t.Fatal("missing named stream limit")
	}
	owners[0].ds.completeMultiplexTurn()
	nextMultiplexObservation(t, started)
}
func TestOpenAIWSMultiplexSharedAuthorizationCloseStopsEveryStream(t *testing.T) {
	started := make(chan multiplexObserved, 2)
	conn := startMultiplexFixture(t, func(ctx context.Context, ds *OpenAIWSDownstream) {
		if _, _, err := ds.Read(ctx); err != nil {
			return
		}
		started <- multiplexObserved{ds: ds}
		<-ds.done
	})
	conn.input <- multiplexFrame("a", "a1", "")
	conn.input <- multiplexFrame("b", "b1", "")
	a, b := nextMultiplexObservation(t, started), nextMultiplexObservation(t, started)
	require.NoError(t, a.ds.CloseConnection(coderws.StatusPolicyViolation, "authorization revoked"))
	for _, ds := range []*OpenAIWSDownstream{a.ds, b.ds} {
		select {
		case <-ds.done:
		case <-time.After(time.Second):
			t.Fatal("shared authorization failure left a stream running")
		}
	}
}

func TestOpenAIWSMultiplexQueueAndUnsupportedControls(t *testing.T) {
	started := make(chan multiplexObserved, 2)
	conn := startMultiplexFixture(t, func(ctx context.Context, ds *OpenAIWSDownstream) {
		_, payload, err := ds.Read(ctx)
		if err != nil {
			return
		}
		stream, event, _ := openAIWSRequestEnvelope(payload)
		started <- multiplexObserved{stream, event, payload, ds}
		<-ds.done
	})
	conn.input <- multiplexFrame("a", "first", "")
	nextMultiplexObservation(t, started)
	for i := 0; i < 17; i++ {
		conn.input <- multiplexFrame("a", fmt.Sprint(i), "")
	}
	select {
	case event := <-conn.writes:
		require.Equal(t, "stream_queue_full", gjson.GetBytes(event, "error.code").String())
	case <-time.After(time.Second):
		t.Fatal("unbounded queue")
	}
	for _, kind := range []string{"response.steer", "response.append"} {
		body, _ := json.Marshal(map[string]any{"type": kind, "stream_id": "a", "event_id": kind})
		conn.input <- body
		select {
		case event := <-conn.writes:
			require.Equal(t, "unsupported_feature", gjson.GetBytes(event, "error.code").String())
			require.Equal(t, kind, gjson.GetBytes(event, "event_id").String())
		case <-time.After(time.Second):
			t.Fatal("missing unsupported error")
		}
	}
	conn.input <- multiplexFrame("b", "healthy", "")
	require.Equal(t, "b", nextMultiplexObservation(t, started).stream)
}
func TestOpenAIWSMultiplexForkRequiresCommittedOwnedHistory(t *testing.T) {
	started := make(chan multiplexObserved, 8)
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "generation1"}}
	conn := startMultiplexFixture(t, func(ctx context.Context, ds *OpenAIWSDownstream) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/v1/responses", nil).WithContext(ctx)
		for {
			_, payload, err := ds.Read(ctx)
			if err != nil {
				return
			}
			if ds.bindRequestEnvelope(payload) != nil {
				return
			}
			stream, event, _ := openAIWSRequestEnvelope(payload)
			prepared, err := ds.prepareMultiplexTurn(ctx, c, account, payload)
			if err != nil {
				_ = ds.Close(coderws.StatusPolicyViolation, err.Error())
				return
			}
			started <- multiplexObserved{stream, event, prepared, ds}
			if event == "parent" {
				body, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_parent", "output": []any{map[string]any{"id": "rs_1", "type": "reasoning", "encrypted_content": "opaque"}}}})
				if ds.Write(ctx, coderws.MessageText, body) != nil {
					return
				}
				ds.completeMultiplexTurn()
			} else {
				<-ds.done
				return
			}
		}
	})
	conn.input <- multiplexFrame("a", "parent", "")
	nextMultiplexObservation(t, started)
	select {
	case <-conn.writes:
	case <-time.After(time.Second):
		t.Fatal("parent not committed")
	}
	conn.input <- multiplexFrame("b", "fork", "resp_parent")
	child := nextMultiplexObservation(t, started)
	require.False(t, gjson.GetBytes(child.payload, "previous_response_id").Exists())
	require.Equal(t, int64(3), gjson.GetBytes(child.payload, "input.#").Int())
	require.Equal(t, "opaque", gjson.GetBytes(child.payload, "input.1.encrypted_content").String())
	// A credential replacement cannot borrow encrypted state from an old turn.
	different := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "generation2"}}
	_, err := child.ds.prepareMultiplexTurn(context.Background(), nil, different, multiplexFrame("b", "bad", "resp_parent"))

	require.ErrorIs(t, err, ErrOpenAIWSReplayUnavailable)
	otherAccount := *account
	otherAccount.ID++
	_, err = child.ds.prepareMultiplexTurn(context.Background(), nil, &otherAccount, multiplexFrame("b", "wrong-account", "resp_parent"))
	require.ErrorIs(t, err, ErrOpenAIWSReplayUnavailable)
	for _, key := range []*APIKey{{ID: 99}, {GroupID: func() *int64 { id := int64(99); return &id }()}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("api_key", key)
		_, err = child.ds.prepareMultiplexTurn(context.Background(), c, account, multiplexFrame("b", "wrong-owner", "resp_parent"))
		require.ErrorIs(t, err, ErrOpenAIWSReplayUnavailable)
	}
}

func TestOpenAIWSMultiplexCancellationBypassesQueuedCreate(t *testing.T) {
	started := make(chan multiplexObserved, 4)
	conn := startMultiplexFixture(t, func(ctx context.Context, ds *OpenAIWSDownstream) {
		ds.controlsEnabled.Store(false)
		ds.modeSelected.Store(true)
		_, payload, err := ds.Read(ctx)
		if err != nil {
			return
		}
		_ = ds.bindRequestEnvelope(payload)
		event, _ := json.Marshal(map[string]any{"type": "response.created", "response": map[string]string{"id": "resp_active"}})
		if ds.Write(ctx, coderws.MessageText, event) != nil {
			return
		}
		started <- multiplexObserved{event: "first", ds: ds}
		_, control, err := ds.Read(ctx)
		if err != nil {
			return
		}
		started <- multiplexObserved{event: gjson.GetBytes(control, "type").String(), ds: ds}
		terminal, _ := json.Marshal(map[string]any{"type": "response.cancelled", "response": map[string]string{"id": "resp_active"}})
		if ds.Write(ctx, coderws.MessageText, terminal) != nil {
			return
		}
		ds.completeMultiplexTurn()
		_, next, err := ds.Read(ctx)
		if err != nil {
			return
		}
		started <- multiplexObserved{event: gjson.GetBytes(next, "event_id").String(), ds: ds}
		<-ds.done
	})
	conn.input <- multiplexFrame("a", "first", "")
	nextMultiplexObservation(t, started)
	conn.input <- multiplexFrame("a", "queued", "")
	for _, target := range []string{"resp_foreign", "resp_active"} {
		control, _ := json.Marshal(map[string]string{"type": "response.cancel", "stream_id": "a", "response_id": target})
		conn.input <- control
	}
	require.Equal(t, "response.cancel", nextMultiplexObservation(t, started).event)
	require.Equal(t, "queued", nextMultiplexObservation(t, started).event)
	found := false
	for i := 0; i < 3; i++ {
		select {
		case event := <-conn.writes:
			if gjson.GetBytes(event, "error.code").String() == "response_not_active" {
				found = true
			}
		case <-time.After(time.Second):
			t.Fatal("missing cancellation event")
		}
	}
	require.True(t, found)
}

type multiplexFailedWriteConn struct {
	*queuedWSDownstreamTestConn
	entered, release chan struct{}
}

func (c *multiplexFailedWriteConn) Write(context.Context, coderws.MessageType, []byte) error {
	close(c.entered)
	<-c.release
	return io.ErrClosedPipe
}
func TestOpenAIWSMultiplexFailedTerminalUnblocksForkWithoutCommit(t *testing.T) {
	conn := &multiplexFailedWriteConn{queuedWSDownstreamTestConn: newQueuedWSDownstreamTestConn(), entered: make(chan struct{}), release: make(chan struct{})}
	m := &openAIWSMultiplexer{conn: conn, ctx: t.Context(), maxBytes: 4096, writeGate: make(chan struct{}, 1), histories: map[string]*openAIWSForkHistory{}}
	parent := &openAIWSStreamConn{mux: m, id: "parent", done: make(chan struct{}), running: true}
	child := &openAIWSStreamConn{mux: m, id: "child", done: make(chan struct{})}
	a := &Account{ID: 1, Credentials: map[string]any{"access_token": "one"}}
	_, err := (&OpenAIWSDownstream{conn: parent}).prepareMultiplexTurn(t.Context(), nil, a, multiplexFrame("parent", "first", ""))
	require.NoError(t, err)
	written := make(chan error, 1)
	go func() {
		written <- parent.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_parent","output":[]}}`))
	}()
	<-conn.entered
	forked := make(chan error, 1)
	go func() {
		_, err := (&OpenAIWSDownstream{conn: child}).prepareMultiplexTurn(t.Context(), nil, a, multiplexFrame("child", "next", "resp_parent"))
		forked <- err
	}()
	close(conn.release)
	require.ErrorIs(t, <-written, io.ErrClosedPipe)
	select {
	case err := <-forked:
		require.ErrorIs(t, err, ErrOpenAIWSReplayUnavailable)
	case <-time.After(time.Second):
		t.Fatal("failed parent publication left fork waiting")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	require.False(t, m.histories["resp_parent"].committed)
}

func TestOpenAIWSMultiplexForkRejectsIncompleteHistory(t *testing.T) {
	for _, tc := range []struct {
		name, input, output string
		blocked             bool
	}{
		{"client_tool_catalog", `[{"type":"additional_tools","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"n":{"const":9007199254740993}}}}]},{"role":"user","content":"hello"}]`, `[]`, false},
		{"complete_message", `"hello"`, `[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]`, false},
		{"encrypted_reasoning", `"hello"`, `[{"id":"rs_1","type":"reasoning","encrypted_content":"cipher"}]`, false},
		{"missing_cipher", `"hello"`, `[{"id":"rs_1","type":"reasoning","summary":[]}]`, true},
		{"unknown_output", `"hello"`, `[{"id":"future_1","type":"future_context","state":"opaque"}]`, true},
		{"untyped_output", `"hello"`, `[{"id":"msg_1","role":"assistant","content":"lost"}]`, true},
		{"nonobject_output", `"hello"`, `[42]`, true},
		{"incomplete_message", `"hello"`, `[{"id":"msg_1","type":"message","role":"assistant"}]`, true},
		{"referenced_input", `[{"type":"item_reference","id":"msg_remote"}]`, `[]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := newQueuedWSDownstreamTestConn()
			m := &openAIWSMultiplexer{conn: conn, ctx: t.Context(), maxBytes: 65536, writeGate: make(chan struct{}, 1), histories: map[string]*openAIWSForkHistory{}}
			parent := &openAIWSStreamConn{mux: m, id: "parent", done: make(chan struct{}), running: true}
			child := &openAIWSStreamConn{mux: m, id: "child", done: make(chan struct{})}
			a := &Account{ID: 1, Credentials: map[string]any{"access_token": "one"}}
			payload := []byte(`{"type":"response.create","model":"gpt-6-luna","store":false,"stream_id":"parent","input":` + tc.input + `}`)
			_, err := (&OpenAIWSDownstream{conn: parent}).prepareMultiplexTurn(t.Context(), nil, a, payload)
			require.NoError(t, err)
			terminal := []byte(`{"type":"response.completed","response":{"id":"resp_parent","status":"completed","output":` + tc.output + `}}`)
			require.NoError(t, parent.Write(t.Context(), coderws.MessageText, terminal))
			replay, err := (&OpenAIWSDownstream{conn: child}).prepareMultiplexTurn(t.Context(), nil, a, multiplexFrame("child", "next", "resp_parent"))
			if tc.blocked {
				require.ErrorIs(t, err, ErrOpenAIWSReplayUnavailable, "incomplete history must not become a full replay")
			} else {
				require.NoError(t, err)
				require.False(t, gjson.GetBytes(replay, "previous_response_id").Exists())
				require.Len(t, gjson.GetBytes(replay, "input").Array(), 3)
			}
		})
	}
}

func TestOpenAIWSReplayCollectorTerminalMustResolvePendingItems(t *testing.T) {
	for _, output := range []string{`[]`, `[{"id":"msg_2","type":"message","role":"assistant","content":"another item"}]`} {
		var collector openAIWSToolCallReplayCollector
		collector.AddEvent("response.output_item.added", []byte(`{"type":"response.output_item.added","item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`))
		collector.AddEvent("response.completed", []byte(`{"type":"response.completed","response":{"output":`+output+`}}`))
		require.Contains(t, collector.pending, "msg_1", "an unrelated or empty terminal output cannot complete a missing item")
	}
	var collector openAIWSToolCallReplayCollector
	collector.AddEvent("response.output_item.added", []byte(`{"item":{"id":"msg_1","type":"message"}}`))
	collector.AddEvent("response.completed", []byte(`{"response":{"output":[{"id":"msg_1","type":"message","role":"assistant","content":"done"}]}}`))
	require.Empty(t, collector.pending)
	require.Len(t, collector.AllItems(), 1)
}

// One rejected named stream must never close or replay an independent stream.
// Exercise the real ingress coordinator, retry adapter, relay and completion
// hooks against local upstreams; the physical client uses a real websocket.
func TestOpenAIWSMultiplexRejectedRepairIsolation(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
		for _, terminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failed=%t", mode, terminal), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				const rejection = `{"code":"unsupported_parameter","type":"invalid_request_error","param":"input","message":"Supplied input item IDs require persisted-item lookup that is not supported by rustponsesapi."}`
				reject := `{"type":"error","error":` + rejection + `}`
				if terminal {
					reject = `{"type":"response.failed","response":{"id":"rejected","status":"failed","error":` + rejection + `}}`
				}
				bad1, bad2, healthy := newStagedPassthroughConn(), newStagedPassthroughConn(), newStagedPassthroughConn()
				var badCalls atomic.Int64
				goodStarted := make(chan struct{}, 2)
				allowGood := make(chan struct{})
				settled := make(chan *OpenAIForwardResult, 4)
				services := map[string]*OpenAIGatewayService{}
				for _, lane := range []string{"bad", "good"} {
					cfg := passthroughLifecycleConfig()
					cfg.Gateway.OpenAIWS.OAuthEnabled = true
					cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
					cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 10
					cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 10
					cfg.Gateway.StreamDataIntervalTimeout = 1 // Fail the rejected stream before the independent output deadline.
					cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 10
					conns := []openAIWSClientConn{&nativeCodexStagedConn{healthy}}
					if lane == "bad" {
						conns = []openAIWSClientConn{&nativeCodexStagedConn{bad1}, &nativeCodexStagedConn{bad2}}
					}
					d := &openAIWSQueueDialer{conns: conns}
					svc := newPassthroughLifecycleService(cfg, healthy)
					svc.openaiWSPassthroughDialer = d
					pool := newOpenAIWSConnPool(cfg)
					pool.setClientDialerForTest(d)
					svc.openaiWSPool = pool
					t.Cleanup(pool.Close)
					currentLane := lane
					turn := 0
					svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							return nil, err
						}
						if currentLane == "bad" {
							n := badCalls.Add(1)
							if n == 2 {
								require.False(t, gjson.GetBytes(body, "input.0.id").Exists())
							}
							return bpsCompletionResponse(200, "data: "+reject+"\n\n"), nil
						}
						turn++
						goodStarted <- struct{}{}
						select {
						case <-allowGood:
						case <-ctx.Done():
							return nil, ctx.Err()
						}
						return bpsCompletionResponse(200, fmt.Sprintf(`data: {"type":"response.completed","response":{"id":"good_%d","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`, turn)+"\n\n"), nil
					}}
					services[lane] = svc
				}
				ended := make(chan error, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						ended <- err
						return
					}
					defer conn.CloseNow()
					ended <- RunOpenAIWSMultiplexer(ctx, conn, services["good"].cfg, func(lctx context.Context, ds *OpenAIWSDownstream) {
						_, first, err := ds.Read(lctx)
						if err != nil {
							return
						}
						lane := gjson.GetBytes(first, "stream_id").String()
						svc := services[lane]
						account := nativeWSTestAccount()
						account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = r.Clone(lctx)
						SetOpenAIClientTransport(c, OpenAIClientTransportWS)
						err = svc.ProxyResponsesWebSocketFromClient(lctx, c, ds, account, "test-token", first, &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, _ error) {
							if result != nil && openAIUsageHasTokens(&result.Usage) {
								settled <- result
							}
						}})
						if err != nil {
							var closeErr *OpenAIWSClientCloseError
							if errors.As(err, &closeErr) {
								_ = ds.Close(closeErr.StatusCode(), closeErr.Reason())
							} else {
								_ = ds.Close(coderws.StatusInternalError, "stream failed")
							}
						}
					})
				}))
				defer server.Close()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
				require.NoError(t, err)
				defer client.CloseNow()
				send := func(lane string) {
					raw := fmt.Sprintf(`{"type":"response.create","stream_id":%q,"model":"gpt-6.1-sol","store":false,"input":[{"type":"message","id":"msg_client","role":"user","content":"hello"}]}`, lane)
					require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(raw)))
				}
				send("good")
				send("bad")
				if mode != OpenAIWSIngressModeHTTPBridge {
					requirePassthroughUpstreamWrite(t, healthy, time.Second)
					requirePassthroughUpstreamWrite(t, bad1, time.Second)
					bad1.Send(reject)
					repaired := requirePassthroughUpstreamWrite(t, bad2, 2*time.Second)
					require.False(t, gjson.GetBytes(repaired, "input.0.id").Exists())
					bad2.Send(reject)
				} else {
					select {
					case <-goodStarted:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				// The failed stream emits its terminal before the healthy stream is released.
				for {
					_, raw, err := client.Read(ctx)
					require.NoError(t, err)
					event := gjson.ParseBytes(raw)
					if event.Get("stream_id").String() == "bad" && (event.Get("type").String() == "error" || event.Get("type").String() == "response.failed") {
						break
					}
				}
				if mode == OpenAIWSIngressModeHTTPBridge {
					require.EqualValues(t, 2, badCalls.Load())
				}
				close(allowGood)
				for turn := 1; turn <= 2; turn++ {
					if mode != OpenAIWSIngressModeHTTPBridge {
						healthy.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"good_%d","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`, turn))
					}
					for {
						_, raw, err := client.Read(ctx)
						require.NoError(t, err)
						event := gjson.ParseBytes(raw)
						if event.Get("stream_id").String() == "good" && event.Get("type").String() == "response.completed" {
							break
						}
					}
					select {
					case result := <-settled:
						require.Equal(t, 3, result.Usage.InputTokens)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if turn == 1 {
						send("good")
						if mode != OpenAIWSIngressModeHTTPBridge {
							requirePassthroughUpstreamWrite(t, healthy, time.Second)
						}
					}
				}
				require.Empty(t, settled, "failed unmetered repair must not create a settlement")
				_ = client.CloseNow()
				select {
				case <-ended:
				case <-ctx.Done():
					t.Fatal("multiplex workers did not exit")
				}
			})
		}
	}
}

func TestOpenAIWSMultiplexFirstReadFailurePhase(t *testing.T) {
	for _, first := range []bool{true, false} {
		t.Run(fmt.Sprint(first), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conn := newQueuedWSDownstreamTestConn()
			done := make(chan error, 1)
			accepted := make(chan struct{})
			go func() {
				done <- RunOpenAIWSMultiplexer(ctx, conn, nil, func(ctx context.Context, ds *OpenAIWSDownstream) {
					_, _, _ = ds.Read(ctx)
					close(accepted)
					<-ctx.Done()
				})
			}()
			if !first {
				conn.input <- multiplexFrame("a", "one", "")
				<-accepted
			}
			cancel()
			_ = conn.CloseNow()
			err := <-done
			var readErr *OpenAIWSFirstMessageError
			require.Equal(t, first, errors.As(err, &readErr))
		})
	}
}
