package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestOpenAIForwardTimingAttemptAndSemanticFlush(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	first := newOpenAIForwardTiming(c, c.Request, &Account{ID: 3})
	second := newOpenAIForwardTiming(c, c.Request, &Account{ID: 3})
	require.Equal(t, 1, first.attempt)
	require.Equal(t, 2, second.attempt)
	require.Same(t, second, openAIForwardTimingFromContext(c))
	core, logs := observer.New(zap.InfoLevel)
	timing := &openAIForwardTiming{started: time.Now(), log: zap.New(core)}
	timing.MarkFlush()
	require.Equal(t, 0, logs.Len(), "heartbeats must not count as semantic flush")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := []byte(`{"type":"response.output_text.delta","delta":"hello"}`)
			timing.ObserveEvent(payload)
			timing.MarkDownstream(payload)
		}()
	}
	wg.Wait()
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_semantic")).Len())
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_downstream_flush")).Len())
	trace := httptrace.ContextClientTrace(timing.context(context.Background()))
	trace.GetConn("sensitive-host")
	trace.GotConn(httptrace.GotConnInfo{Reused: true})
	trace.WroteRequest(httptrace.WroteRequestInfo{Err: errors.New("secret proxy credential")})
	trace.GotFirstResponseByte()
	timing.markResponse(&http.Response{StatusCode: 502}, errors.New("secret token"))
	for _, entry := range logs.All() {
		require.NotContains(t, entry.ContextMap(), "error")
		require.NotContains(t, entry.Message, "secret")
	}
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_response_byte")).Len())
	var absent *openAIForwardTiming
	absent.MarkSemantic()
	absent.MarkFlush()
}

func TestOpenAIUpstreamJSONOrderPreservesValues(t *testing.T) {
	original := []byte(`{"model":"old","input":[1],"unknown":true}`)
	encoded := []byte(`{"input":[9007199254740993,{"escaped":"<x>中文"}],"stream":true,"model":"new","unknown":true,"client_metadata":{"a":1}}`)
	reordered := restoreOpenAIUpstreamJSONTopLevelOrder(original, encoded)
	require.Equal(t, []string{"model", "input", "unknown", "stream", "client_metadata"}, openAIUpstreamJSONTopLevelKeys(reordered))
	require.Contains(t, string(reordered), "9007199254740993")
	require.Contains(t, string(reordered), "<x>中文")
	require.JSONEq(t, string(encoded), string(reordered))
	require.Equal(t, encoded, restoreOpenAIUpstreamJSONTopLevelOrder([]byte("invalid"), encoded))
	require.Equal(t, []byte("[1]"), restoreOpenAIUpstreamJSONTopLevelOrder(original, []byte("[1]")))
	duplicate := []byte(`{"input":[1],"input":[2],"model":"new"}`)
	require.Equal(t, duplicate, restoreOpenAIUpstreamJSONTopLevelOrder(original, duplicate), "unexpected duplicate keys must not truncate the object")
}

func TestOpenAIForwardTimingCompleteSSEAndActualFlush(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	timing := &openAIForwardTiming{started: time.Now(), log: zap.New(core)}
	text := []byte(`{"type":"response.output_text.delta","delta":"secret words"}`)
	timing.queueSSEEvent(text)
	timing.stageSSEDownstream(text)
	timing.MarkFlush()
	require.Zero(t, logs.Len(), "partial SSE event and heartbeat cannot claim first text")
	timing.completeSSEEvent()
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_text")).Len())
	timing.MarkFlush()
	require.Zero(t, logs.FilterField(zap.String("stage", "first_text_flush")).Len(), "upstream read-ahead is not a downstream write")
	timing.completeSSEDownstream()
	timing.MarkFlush()
	timing.MarkFlush()
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_text_flush")).Len())
	terminal := []byte(`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":20,"output_tokens":5,"output_tokens_details":{"reasoning_tokens":3}}}}`)
	timing.ObserveEvent(terminal)
	timing.MarkFlush()
	require.Zero(t, logs.FilterField(zap.String("stage", "terminal_output_flush")).Len(), "observed terminal is not a downstream flush")
	timing.stageSSEDownstream(terminal)
	timing.completeSSEDownstream()
	require.Zero(t, logs.FilterField(zap.String("stage", "terminal_output_flush")).Len())
	timing.MarkFlush()
	timing.MarkFlush()
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "terminal_output_flush")).Len())
	timing.Finish(nil)
	timing.Finish(context.Canceled)
	require.Equal(t, 1, logs.FilterField(zap.String("stage", "attempt_summary")).Len())
	summary := logs.FilterField(zap.String("stage", "attempt_summary")).All()[0].ContextMap()
	require.Equal(t, "completed", summary["outcome"])
	require.Equal(t, false, summary["body_wire_bytes_known"])
	require.NotContains(t, summary, "body_wire_bytes")
	encoded, err := json.Marshal(logs.All())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret words")
}
func TestOpenAIForwardTimingTextDefinition(t *testing.T) {
	for _, tc := range []struct {
		body          string
		text, content bool
	}{
		{`{"type":"response.created"}`, false, false},
		{`{"type":"response.output_text.delta","delta":""}`, false, false},
		{`{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`, false, true},
		{`{"type":"response.function_call_arguments.delta","delta":"{}"}`, false, true},
		{`{"type":"response.output_text.delta","delta":"a"}`, true, true},
		{`{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}}`, true, true},
	} {
		e := classifyOpenAITimingEvent([]byte(tc.body))
		require.Equal(t, tc.text, e.text, tc.body)
		require.Equal(t, tc.content, e.content, tc.body)
	}
}

func TestAnthropicStreamTimingDoesNotCountHeartbeatAsText(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		c, logs := newWSLatencyTestContext()
		body := "data: {\"type\":\"ping\"}\n\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-test\",\"usage\":{\"input_tokens\":7}}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"private reasoning\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"private answer\"}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n"
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
		svc := &GatewayService{rateLimitService: &RateLimitService{}}
		account := &Account{ID: 3, Platform: PlatformAnthropic}
		var err error
		if passthrough {
			_, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(c.Request.Context(), resp, c, account, time.Now().Add(-time.Hour), "requested-claude")
		} else {
			_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now().Add(-time.Hour), "requested-claude", "claude-test", false)
		}
		require.NoError(t, err)
		summaries := wsLatencySummaries(logs)
		require.Len(t, summaries, 1)
		require.Equal(t, "completed", summaries[0]["outcome"])
		require.Equal(t, "anthropic", summaries[0]["protocol"])
		require.Equal(t, "response_stream", summaries[0]["timing_scope"])
		require.Equal(t, "requested-claude", summaries[0]["model"])
		require.Equal(t, "claude-test", summaries[0]["actual_model"])
		require.Less(t, summaries[0]["elapsed_ms"].(int64), int64(time.Minute/time.Millisecond), "stream metrics must not inherit the caller's earlier request start")
		require.NotContains(t, summaries[0], "request_elapsed_ms")
		stages := []string{}
		for _, entry := range logs.All() {
			stage, _ := entry.ContextMap()["stage"].(string)
			stages = append(stages, stage)
			encoded, marshalErr := json.Marshal(entry.ContextMap())
			require.NoError(t, marshalErr)
			require.NotContains(t, string(encoded), "private")
		}
		require.Subset(t, stages, []string{"first_event", "first_semantic", "first_text", "first_text_flush", "terminal"})
		require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_text")).Len())
		require.False(t, classifyOpenAITimingEvent([]byte(`{"type":"ping"}`)).text)
		require.False(t, classifyOpenAITimingEvent([]byte(`{"type":"message_start"}`)).semantic)
	}
}
func TestOpenAIForwardTimingBufferedUnknownAndEncoding(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	timing := &openAIForwardTiming{started: time.Now(), log: zap.New(core)}
	timing.SetRequestMetadata([]byte(`{"model":"gpt-test","reasoning":{"effort":"high"},"service_tier":"priority","input":[]}`))
	ctx := timing.context(context.Background())
	RecordOpenAIRequestBodyEncoding(ctx, 1000, 250, "zstd", time.Millisecond)
	timing.ObserveBufferedResponse([]byte(`{"object":"response","status":"completed","model":"gpt-test","service_tier":"default","output":[{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}`))
	timing.MarkDownstream([]byte(`{"object":"response","status":"completed","output":[]}`))
	timing.Finish(nil)
	summary := logs.FilterField(zap.String("stage", "attempt_summary")).All()[0].ContextMap()
	require.Equal(t, "completed", summary["outcome"])
	require.Equal(t, int64(250), summary["body_wire_bytes"])
	require.Equal(t, "priority", summary["requested_service_tier"])
	require.Equal(t, "default", summary["actual_service_tier"])
	require.NotContains(t, summary, "upstream_usage", "missing usage is unknown, not zero")
	require.Zero(t, logs.FilterField(zap.String("stage", "first_text")).Len())
	require.Zero(t, logs.FilterField(zap.String("stage", "first_text_flush")).Len())
}
func TestOpenAIForwardTimingTurnsAttemptsAndWaits(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	BeginOpenAIRequestTiming(c)
	root := openAIRequestTimingFromContext(c.Request.Context())
	root.started = time.Now().Add(-time.Second)
	MarkOpenAIRequestTimingStage(c, "auth_done")
	MarkOpenAIRequestTimingStage(c, "account_slot")
	factory := newOpenAITimingFactory(c, &Account{ID: 1})
	core, logs := observer.New(zap.InfoLevel)
	factory.log = zap.New(core)
	first := factory.BeginTurn(1, 100)
	first.Finish(errors.New("failed first attempt"))
	second := factory.BeginTurn(1, 100)
	require.Equal(t, 2, second.attempt)
	require.False(t, root.finished)
	second.ObserveEvent([]byte(`{"type":"response.completed","response":{"output":[]}}`))
	second.FinishTurn(nil)
	third := factory.BeginTurn(2, 200)
	third.FinishTurn(context.Canceled)
	factory.Finish(nil)
	require.Equal(t, 1, third.attempt)
	require.NotSame(t, root, third.root)
	summaries := logs.FilterMessage("openai.request_timing").All()
	require.Len(t, summaries, 2)
	require.GreaterOrEqual(t, summaries[0].ContextMap()["request_duration_ms"].(int64), int64(1000))
	require.Equal(t, int64(2), summaries[0].ContextMap()["attempts"])
	require.Equal(t, true, summaries[1].ContextMap()["canceled"])
}
func TestOpenAIForwardTimingRealHTTPStreams(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "passthrough"}[passthrough], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			svc := &OpenAIGatewayService{}
			timing := newOpenAITransportTiming(c, account, "http", 10)
			core, logs := observer.New(zap.InfoLevel)
			timing.log = zap.New(core)
			body := "data: {\"type\":\"response.created\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-test", "gpt-test")
			} else {
				_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-test", "gpt-test")
			}
			require.NoError(t, err)
			timing.Finish(err)
			require.Contains(t, recorder.Body.String(), "hello")
			require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_text")).Len())
			require.Equal(t, 1, logs.FilterField(zap.String("stage", "first_text_flush")).Len())
			require.Equal(t, "completed", logs.FilterField(zap.String("stage", "attempt_summary")).All()[0].ContextMap()["outcome"])
		})
	}
}

func TestOpenAIForwardTimingHTTPObservationPreservesRequest(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{true: "sse", false: "json"}[stream], func(t *testing.T) {
			svc, c, account, opts := incrementalFixture(t)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			c.Set(openAIIncrementalOriginalScopeKey, opts.ExecutionScope)
			upstream := &httpUpstreamSequenceRecorder{}
			svc.httpUpstream = upstream
			for i, items := range []string{incrementalUser, incrementalUser + "," + incrementalOutput + "," + incrementalNext} {
				body := incrementalRequest(items)
				response := `{"object":"response","id":"resp_p2","status":"completed","output":[` + incrementalOutput + `],"usage":{"input_tokens":1,"output_tokens":2}}`
				contentType := "application/json"
				if stream {
					contentType = "text/event-stream"
					response = "data: " + string(incrementalCompleted(incrementalOutput)) + "\n\n"
				}
				upstream.responses = append(upstream.responses, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))})
				req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, opts.Endpoint, strings.NewReader(string(body)))
				require.NoError(t, err)
				req.Header.Set("Authorization", opts.CredentialGeneration)
				resp, err := svc.doOpenAIResponsesUpstream(c, req, "", account, stream)
				require.NoError(t, err)
				timing := openAIForwardTimingFromContext(c)
				require.NotNil(t, timing.observation)
				if stream {
					_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
				} else {
					_, err = svc.handleNonStreamingResponse(c.Request.Context(), resp, c, account, "gpt-5.5", "gpt-5.5")
				}
				require.NoError(t, err)
				timing.Finish(err)
				require.Equal(t, "completed", timing.observation.report.AnchorReason)
				if i == 1 {
					require.Equal(t, "true", timing.observation.report.PrefixMatch)
					require.Equal(t, "unknown", timing.observation.report.StateReady)
				}
				require.Equal(t, body, upstream.bodies[i])
				require.Equal(t, i+1, upstream.callCount, "observation must never send a probe")
			}
		})
	}
}

func TestOpenAIForwardTimingContextPreservesTurnAndCancellation(t *testing.T) {
	type scopeKey struct{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	BeginOpenAIRequestTiming(c, time.Now().Add(-time.Hour))
	old := openAIRequestTimingFromContext(c.Request.Context())
	BeginOpenAIRequestTiming(c, time.Now())
	current := openAIRequestTimingFromContext(c.Request.Context())
	require.NotSame(t, old, current)
	require.Less(t, time.Since(current.started), time.Second)
	connection, cancel := context.WithCancel(context.WithValue(c.Request.Context(), scopeKey{}, "billing-and-admission"))
	BeginOpsStreamTurn(c, 2)
	next := connection
	require.Equal(t, 2, openAIRequestTimingFromContext(next).turn)
	require.Equal(t, "billing-and-admission", next.Value(scopeKey{}))
	cancel()
	require.ErrorIs(t, next.Err(), context.Canceled)
}

func TestOpenAIForwardTimingDoesNotPublishUnprovenAnchor(t *testing.T) {
	for _, kind := range []string{"buffered_sse", "stream_write_failure", "json_write_failure"} {
		t.Run(kind, func(t *testing.T) {
			svc, c, account, opts := incrementalFixture(t)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			c.Set(openAIIncrementalOriginalScopeKey, opts.ExecutionScope)
			timing := newOpenAITransportTiming(c, account, "http", 10)
			svc.attachOpenAIIncrementalObservation(c.Request.Context(), c, account, incrementalRequest(incrementalUser), opts.Endpoint, http.Header{"Authorization": []string{opts.CredentialGeneration}}, "", timing)
			response := `{"object":"response","id":"resp_p2","status":"completed","output":[` + incrementalOutput + `],"usage":{"input_tokens":1,"output_tokens":2}}`
			if kind != "json_write_failure" {
				response = "data: " + string(incrementalCompleted(incrementalOutput)) + "\n\n"
			}
			header := http.Header{"Content-Type": []string{"text/event-stream"}}
			if kind == "json_write_failure" {
				header.Set("Content-Type", "application/json")
			}
			resp := &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(response))}
			if kind != "buffered_sse" {
				c.Writer = &failingGinWriter{ResponseWriter: c.Writer, failAfter: 0}
			}
			var err error
			if kind == "stream_write_failure" {
				_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
			} else {
				_, err = svc.handleNonStreamingResponse(c.Request.Context(), resp, c, account, "gpt-5.5", "gpt-5.5")
			}
			timing.Finish(err)
			require.NotEqual(t, "completed", timing.observation.report.AnchorReason)
			if kind == "buffered_sse" {
				require.Equal(t, "output_incomplete", timing.observation.report.AnchorReason)
			} else {
				require.True(t, timing.downstreamFailed)
			}
		})
	}
}

func TestOpenAIForwardTimingRelayKeepsAcceptedTurnWait(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	factory := newOpenAITimingFactory(c, &Account{ID: 1})
	first := factory.BeginTurn(1, 10)
	first.FinishTurn(nil)
	BeginOpsStreamTurn(c, 2)
	accepted := openAIRequestTimingFromContext(c.Request.Context())
	accepted.started = time.Now().Add(-time.Second)
	accepted.mark("user_slot", time.Now())
	second := factory.BeginTurn(2, 10)
	require.Same(t, accepted, second.root)
	require.GreaterOrEqual(t, second.root.stages["user_slot"], int64(1000))
	factory.Finish(nil)
}

func TestOpenAIForwardTimingPluginExcludesNativeEvidence(t *testing.T) {
	svc, c, account, opts := incrementalFixture(t)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	c.Set(openAIIncrementalOriginalScopeKey, opts.ExecutionScope)
	svc.pluginManager = &PluginManager{}
	svc.pluginManager.route.Store(&pluginRoute{rolloutPercent: 100, unavailable: "fixture"})
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", opts.Endpoint, strings.NewReader(string(incrementalRequest(incrementalUser))))
	require.NoError(t, err)
	req.Header.Set("Authorization", opts.CredentialGeneration)
	_, err = svc.doOpenAIResponsesUpstream(c, req, "", account, false)
	require.Error(t, err)
	timing := openAIForwardTimingFromContext(c)
	require.Equal(t, "plugin", timing.transport)
	require.False(t, timing.wireKnown)
	require.Equal(t, "opaque_transport", timing.observation.report.Reason)
	require.False(t, timing.observation.report.Candidate)
}
