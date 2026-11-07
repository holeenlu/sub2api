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

type cloudDirectEventTestWriter struct {
	gin.ResponseWriter
	events    [][]byte
	rawWrites int
}

func (w *cloudDirectEventTestWriter) Write(p []byte) (int, error) {
	w.rawWrites++
	return 0, errors.New("Cloud WS must not write HTTP or SSE bytes")
}
func (w *cloudDirectEventTestWriter) WriteResponseEvent(p []byte) error {
	w.events = append(w.events, append([]byte(nil), p...))
	return nil
}

func TestCloudWSWritesResponsesEventsWithoutSSE(t *testing.T) {
	svc, input, _, wire := cloudForwardFixture(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx, err := svc.CaptureCloudBillingSnapshot(ctx, input)
	require.NoError(t, err)
	ctx = context.WithValue(ctx, cloudWebsocketModelKey{}, "client-model")
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireStart(t, ctx, wire)
		cloudWireComplete(wire)
	}()
	defer func() { cancel(); <-done }()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", input.APIKey)
	SetOpenAIHTTPResponseOwner(c, input.User.ID, input.APIKey.ID)
	writer := &cloudDirectEventTestWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	body := []byte(`{"model":"gpt-5.1","input":"hello","stream":true}`)
	ctx = cloudFixtureBindDirect(t, svc, input, ctx, c, body, "client-model", "ws")
	result, err := svc.forwardOpenAICloud(ctx, c, input.Account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Zero(t, writer.rawWrites)
	require.NotEmpty(t, writer.events)
	require.Equal(t, "response.completed", gjson.GetBytes(writer.events[len(writer.events)-1], "type").String())
	require.Equal(t, "client-model", gjson.GetBytes(writer.events[len(writer.events)-1], "response.model").String())
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
}

func TestResponsesEventSinkPreservesExtensionsAndBounds(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	var events [][]byte
	writer := &responsesEventWriter{ResponseWriter: c.Writer, header: make(http.Header), size: -1,
		context: c, maxFrameBytes: 256, startDrain: func() {},
		write: func(raw []byte) error { events = append(events, append([]byte(nil), raw...)); return nil }}
	raw := []byte(`{"type":"response.future_event","extension":{"integer":9007199254740993123456789,"nested":"data: text\n\n"}}`)
	require.NoError(t, writer.WriteResponseEvent(raw))
	require.Equal(t, [][]byte{raw}, events, "raw unknown fields must survive without a JSON/SSE round trip")
	require.Empty(t, writer.pending)
	require.Error(t, writer.WriteResponseEvent(make([]byte, 257)))
	require.Len(t, events, 1)
}
