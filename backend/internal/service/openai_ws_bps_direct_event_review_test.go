package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bpsDirectEventReviewWriter struct {
	gin.ResponseWriter
	events      [][]byte
	rawWrites   int
	rejectKind  string
	rejectError error
}

func (w *bpsDirectEventReviewWriter) Write(p []byte) (int, error) {
	w.rawWrites++
	return 0, errors.New("BPS event output must not write SSE or HTTP bytes")
}
func (w *bpsDirectEventReviewWriter) WriteString(p string) (int, error) { return w.Write([]byte(p)) }
func (w *bpsDirectEventReviewWriter) WriteResponseEvent(p []byte) error {
	w.ResponseWriter.WriteHeaderNow()
	if w.rejectError != nil && gjson.GetBytes(p, "type").String() == w.rejectKind {
		return w.rejectError
	}
	w.events = append(w.events, bytes.Clone(p))
	return nil
}

func bpsDirectEventReviewWire(t *testing.T, complete bool) string {
	t.Helper()
	var wire strings.Builder
	emit := func(kind string, fields map[string]any) {
		fields["type"] = kind
		raw, err := json.Marshal(fields)
		require.NoError(t, err)
		fmt.Fprintf(&wire, "event: %s\ndata: %s\n\n", kind, raw)
	}
	emit("response.created", map[string]any{"response": map[string]any{"id": "resp_direct_output", "model": "gpt-6-astra", "created_at": 1700000000, "status": "in_progress", "output": []any{}}})
	emit("response.in_progress", map[string]any{"response": map[string]any{"id": "resp_direct_output", "model": "gpt-6-astra", "created_at": 1700000000, "status": "in_progress", "output": []any{}}})
	emit("response.output_text.delta", map[string]any{"item_id": "message", "output_index": 0, "content_index": 0, "delta": "first line\ndata: literal\n\nlast line"})
	if complete {
		emit("response.completed", map[string]any{"response": map[string]any{"id": "resp_direct_output", "model": "gpt-6-astra", "created_at": 1700000000, "status": "completed", "output": []any{map[string]any{"type": "message", "id": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": "first line\ndata: literal\n\nlast line"}}}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}}})
	}
	return wire.String()
}

func TestBPSDirectResponsesEventsPreserveStagingAndTerminal(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(fmt.Sprintf("complete_%t", complete), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(bpsDirectEventReviewWire(t, complete)))}}
			svc := excelBPSGatewayTestService(upstream)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			writer := &bpsDirectEventReviewWriter{ResponseWriter: c.Writer}
			c.Writer = writer
			body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": true, "input": "hello"})
			require.NoError(t, err)
			result, err := svc.Forward(t.Context(), c, excelAccount(), body)
			if complete {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NotNil(t, result)
			require.Zero(t, writer.rawWrites)
			require.Len(t, upstream.requests, 1, "delivered output cannot start another generation")
			require.GreaterOrEqual(t, len(writer.events), 4)
			require.Equal(t, "response.created", gjson.GetBytes(writer.events[0], "type").String())
			terminal := "response.completed"
			if !complete {
				terminal = "response.failed"
			}
			require.Equal(t, terminal, gjson.GetBytes(writer.events[len(writer.events)-1], "type").String())
			var text strings.Builder
			for _, event := range writer.events {
				require.True(t, json.Valid(event), "output receives JSON event payloads, never SSE")
				if gjson.GetBytes(event, "type").String() == "response.output_text.delta" {
					text.WriteString(gjson.GetBytes(event, "delta").String())
				}
			}
			require.Equal(t, "first line\ndata: literal\n\nlast line", text.String())
			if complete {
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
			}
		})
	}
}

func TestBPSDirectResponsesEventFailureStopsDeliveryWithoutReplay(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(bpsDirectEventReviewWire(t, true)))}}
	svc := excelBPSGatewayTestService(upstream)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	failure := errors.New("local event sink unavailable")
	writer := &bpsDirectEventReviewWriter{ResponseWriter: c.Writer, rejectKind: "response.output_text.delta", rejectError: failure}
	c.Writer = writer
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": true, "input": "hello"})
	require.NoError(t, err)
	result, err := svc.Forward(t.Context(), c, excelAccount(), body)
	require.ErrorIs(t, err, failure)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Zero(t, writer.rawWrites)
	require.Len(t, upstream.requests, 1)
	for _, event := range writer.events {
		require.NotEqual(t, "response.completed", gjson.GetBytes(event, "type").String())
	}
}

func TestBPSDirectResponsesContextWindowFailureUsesEventSink(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	writer := &bpsDirectEventReviewWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	markOpenAIResponsesClientStream(c, true)
	require.True(t, writeOpenAIContextWindowSSEFailure(c, http.StatusBadRequest, "context window exceeded", "input"))
	require.Zero(t, writer.rawWrites)
	require.Len(t, writer.events, 1)
	require.Equal(t, "response.failed", gjson.GetBytes(writer.events[0], "type").String())
	require.Equal(t, "context_length_exceeded", gjson.GetBytes(writer.events[0], "response.error.code").String())
	require.Equal(t, "input", gjson.GetBytes(writer.events[0], "response.error.param").String())
}
