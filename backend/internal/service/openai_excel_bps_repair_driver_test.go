package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bpsRepairDriverInput struct {
	io.ReadCloser
	closes atomic.Int32
}

func (b *bpsRepairDriverInput) Close() error {
	b.closes.Add(1)
	return b.ReadCloser.Close()
}

func TestExcelBPSRepairDriverDirectEventsBackpressureAndClose(t *testing.T) {
	observed := make(chan struct{}, 2)
	var count atomic.Int32
	bridge := &basispoints.Bridge{OnUpstreamEvent: func([]byte) {
		count.Add(1)
		observed <- struct{}{}
	}}
	wire := strings.Repeat("data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\"}}\n\n", 20)
	input := &bpsRepairDriverInput{ReadCloser: io.NopCloser(strings.NewReader(wire))}
	body := (&excelBPSRepairDriver{bridge: bridge}).stream(t.Context(), input, nil, nil)
	t.Cleanup(func() { _ = body.Close() })
	awaitObserved := func() {
		t.Helper()
		select {
		case <-observed:
		case <-time.After(2 * time.Second):
			t.Fatal("converter did not reach the event handoff")
		}
	}
	awaitObserved()
	require.Equal(t, int32(1), count.Load(), "a stalled consumer must stop conversion at its first event")
	require.True(t, body.Next(t.Context(), nil, nil))
	event := body.Event()
	require.Equal(t, "response.in_progress", event.kind)
	require.True(t, json.Valid(event.payload))
	awaitObserved()
	require.Equal(t, int32(2), count.Load())
	closed := make(chan error, 1)
	go func() { closed <- body.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel and join a blocked event delivery")
	}
	require.Equal(t, int32(1), input.closes.Load())
	require.Equal(t, int32(2), count.Load(), "closing cannot resume parsing or send another generation")
	require.Empty(t, body.ObservedUsage(), "an interrupted unmetered stream remains unknown")
	select {
	case <-body.done:
	default:
		t.Fatal("Close returned before the producer joined")
	}
}

func TestExcelBPSRepairDriverDirectEventsCancellationInterruptsInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	input := &bpsRepairDriverInput{ReadCloser: reader}
	body := (&excelBPSRepairDriver{bridge: &basispoints.Bridge{}}).stream(ctx, input, nil, nil)
	t.Cleanup(func() { _ = body.Close() })
	cancel()
	require.False(t, body.Next(ctx, nil, nil))
	require.ErrorIs(t, body.Err(), context.Canceled)
	closed := make(chan error, 1)
	go func() { closed <- body.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not unblock upstream reading and join")
	}
	require.Equal(t, int32(1), input.closes.Load())
}

func TestExcelBPSRepairDriverDirectCompactCancellationRetainsGenerationUsage(t *testing.T) {
	_, bridge, err := basispoints.Prepare([]byte(`{"model":"gpt-6-astra","stream":true,"input":"calculate","tools":[{"type":"custom","name":"exec"}]}`), "compact-cancel", nil)
	require.NoError(t, err)
	wire := excelBPSRepairWire(t, "initial", "codex2api.custom/exec", "text(42);")
	input := &bpsRepairDriverInput{ReadCloser: io.NopCloser(strings.NewReader(wire))}
	window := []any{map[string]any{"type": "compaction", "id": "cmp", "encrypted_content": "opaque"}}
	body := (&excelBPSRepairDriver{bridge: bridge}).stream(t.Context(), input, window, map[string]any{"input_tokens": json.Number("7")})
	t.Cleanup(func() { _ = body.Close() })
	require.True(t, body.Next(t.Context(), nil, nil))
	require.Equal(t, "compaction", gjson.GetBytes(body.Event().payload, "item.type").String())
	require.NoError(t, body.Close())
	// The terminal/repair facts exist before window/tool output is delivered;
	// canceling this projection must not erase or add to generation's charge.
	require.Equal(t, json.Number("10"), body.ObservedUsage()["input_tokens"])
	require.Equal(t, int32(1), input.closes.Load())
}

func TestExcelBPSDirectSemanticEventsReachHTTPAndEventConsumers(t *testing.T) {
	for _, output := range []string{"http_sse", "events"} {
		t.Run(output, func(t *testing.T) {
			const extra = `{"large":90071992547409931234567890,"opaque":"data: literal\n\n<state>","null":null}`
			wire := "event: response.created\r\ndata: {\"response\":{\"id\":\"r\",\"output\":[]},\"extension\":" + extra + "}\r\n\r\n" +
				"data: {\"type\":\"response.completed\",\"extension\":" + extra + ",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":7},\"extension\":" + extra + "}}\n\n"
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, wire)}
			svc := excelBPSGatewayTestService(upstream)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			var sink *bpsDirectEventReviewWriter
			if output == "events" {
				sink = &bpsDirectEventReviewWriter{ResponseWriter: c.Writer}
				c.Writer = sink
			}
			result, err := svc.Forward(t.Context(), c, excelAccount(), []byte(`{"model":"gpt-6-astra","stream":true,"input":"hello"}`))
			require.NoError(t, err)
			require.Equal(t, 7, result.Usage.InputTokens)
			require.Len(t, upstream.requests, 1)
			var events [][]byte
			if sink != nil {
				require.Zero(t, sink.rawWrites)
				events = sink.events
			} else {
				require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					if strings.HasPrefix(line, "data: ") {
						events = append(events, []byte(strings.TrimPrefix(line, "data: ")))
					}
				}
			}
			require.Len(t, events, 2)
			for i, event := range events {
				require.True(t, json.Valid(event))
				require.EqualValues(t, i, gjson.GetBytes(event, "sequence_number").Int())
				require.Equal(t, "90071992547409931234567890", gjson.GetBytes(event, "extension.large").Raw)
				require.Equal(t, "data: literal\n\n<state>", gjson.GetBytes(event, "extension.opaque").String())
			}
			require.Equal(t, "response.created", gjson.GetBytes(events[0], "type").String())
			require.Equal(t, "response.completed", gjson.GetBytes(events[1], "type").String())
			require.Equal(t, "90071992547409931234567890", gjson.GetBytes(events[1], "response.extension.large").Raw)
		})
	}
}

func TestExcelBPSRepairDriverCancellationClosesLateResponseAndJoins(t *testing.T) {
	for _, mode := range []string{"transport", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			source := []byte("{\"model\":\"gpt-6-astra\",\"input\":\"calculate\",\"tools\":[{\"type\":\"custom\",\"name\":\"exec\"}]}")
			prepared, bridge, err := basispoints.Prepare(source, "driver-scope", nil)
			require.NoError(t, err)
			code := "text(42);"
			if mode == "unknown" {
				code = "{\"name\":\"missing\",\"arguments\":{}}"
			}
			initial := &bpsRepairDriverInput{ReadCloser: io.NopCloser(strings.NewReader(excelBPSRepairWire(t, "initial", "Run", code)))}
			late := &bpsRepairDriverInput{ReadCloser: io.NopCloser(strings.NewReader(excelBPSRepairWire(t, "late", "codex2api.custom/exec", "text(42);")))}
			started := make(chan struct{})
			var sends atomic.Int32
			upstream := &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
				sends.Add(1)
				if initial.closes.Load() != 1 {
					t.Error("preceding concurrency lease must be released before the send")
				}
				close(started)
				<-req.Context().Done()
				// A transport can hand off headers just as cancellation arrives.
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: late}, nil
			}}
			svc := excelBPSGatewayTestService(&upstream.httpUpstreamRecorder)
			svc.httpUpstream = upstream
			diagnosticResponse := &http.Response{StatusCode: 200, Header: http.Header{}}
			diagnosticBody, stage := prepared, "generation"
			driver := &excelBPSRepairDriver{service: svc, account: excelAccount(), bridge: bridge, scope: "driver-scope", token: "test-token", accountID: "test-account", model: "gpt-6-astra", prepared: prepared, diagnosticResponse: &diagnosticResponse, diagnosticBody: &diagnosticBody, diagnosticStage: &stage, observeOwnership: func(*basispoints.UpstreamFailure) bool { return false }}
			body := driver.stream(t.Context(), initial, nil, nil)
			t.Cleanup(func() { _ = body.Close() })
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("repair send did not start")
			}
			closed := make(chan error, 1)
			go func() { closed <- body.Close() }()
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("consumer close did not join the repair driver")
			}
			require.Equal(t, int32(1), sends.Load())
			require.Equal(t, int32(1), initial.closes.Load())
			require.Equal(t, int32(1), late.closes.Load())
			require.NoError(t, body.Close())
			require.Equal(t, int32(1), late.closes.Load(), "repeated close cannot release another lease")
			usage := body.ObservedUsage()
			require.NotEmpty(t, usage)
			// Original metered work survives the interrupted correction.
			require.Contains(t, usage, "input_tokens")
		})
	}
}

func TestExcelBPSRepairDriverPreparedHistoryAndUnknownDemand(t *testing.T) {
	for _, kind := range []basispoints.RepairKind{basispoints.RepairToolTransport, basispoints.RepairUnknownContinuation, basispoints.RepairUnknownTarget} {
		t.Run(string(kind), func(t *testing.T) {
			// The builder remains pure; the application uses the exact prepared
			// attachment and scope fields instead of preparing another request.
			prepared := []byte("{\"input\":[{\"role\":\"user\",\"content\":[{\"type\":\"input_image\",\"file_id\":\"file-owned\"}]}],\"metadata\":{\"agent_iteration\":\"1\",\"task_id\":\"task-fixed\"},\"model\":\"gpt-6-astra\"}")
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, excelBPSRepairWire(t, "fixed", "codex2api.custom/exec", "text(42);"))}
			svc := excelBPSGatewayTestService(upstream)
			var resp *http.Response
			var wire []byte
			var stage string
			driver := &excelBPSRepairDriver{service: svc, account: excelAccount(), bridge: &basispoints.Bridge{}, scope: "fixed-scope", token: "test-token", accountID: "test-account", prepared: prepared, diagnosticResponse: &resp, diagnosticBody: &wire, diagnosticStage: &stage, observeOwnership: func(*basispoints.UpstreamFailure) bool { return false }}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream := &excelBPSRepairStream{}
			t.Cleanup(func() { _ = stream.closeActive() })
			raw := excelBPSRepairWire(t, "bad", "Run", "text(42);")
			response := gjson.Get(strings.Split(raw, "data: ")[1], "response")
			// Decode through the existing protocol reader to preserve JSON integers.
			failed, err := basispoints.ReadToolRepairResponse(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":" + response.Raw + "}\n\n"))
			require.NoError(t, err)
			_, err = driver.repair(ctx, stream, &basispoints.RepairDemand{Kind: kind, FailedResponse: failed, Validation: io.ErrUnexpectedEOF})
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "file-owned", gjson.GetBytes(wire, "input.0.content.0.file_id").String())
			require.Equal(t, "task-fixed", gjson.GetBytes(wire, "metadata.task_id").String())
			require.Equal(t, "gpt-6-astra", gjson.GetBytes(wire, "model").String())
			if kind == basispoints.RepairUnknownTarget {
				require.Equal(t, "tool_regeneration", stage)
				require.NotContains(t, string(wire), "function_call_output")
			} else {
				require.Equal(t, "tool_repair", stage)
				require.Contains(t, string(wire), "executed")
				require.Equal(t, "2", gjson.GetBytes(wire, "metadata.agent_iteration").String())
			}
		})
	}
}
