package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func nativeHTTPObservedFixture(t *testing.T, wire, contentType string, families ...nativeUpstreamFamily) (*nativeOperationFixture, *nativeHTTPOperation, *gin.Context) {
	t.Helper()
	f := newNativeOperationFixture(t, true, 3)
	family := nativeUpstreamResponses
	if len(families) > 0 {
		family = families[0]
	}
	f.ctx = withNativeUpstreamFamily(f.ctx, family)
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}, "X-Request-Id": {"request-real"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(f.ctx)
	return f, nativeHTTPFromContext(f.ctx), c
}

func TestNativeHTTPActualNextFactsAndDeliveryReceipt(t *testing.T) {
	wire := "{\"id\":\"resp-real\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"returned-model\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2},\"extension\":{\"retain\":true}}"
	f, owner, c := nativeHTTPObservedFixture(t, wire, "application/json")
	RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
		response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Equal(t, wire, string(body), "Next must preserve extensions and exact wire bytes")
		require.NoError(t, response.Body.Close())
		c.Writer.Header().Set("Content-Type", "application/json")
		n, err := c.Writer.Write(body)
		require.NoError(t, err)
		require.Equal(t, len(body), n)
		require.NoError(t, http.NewResponseController(c.Writer).Flush())
		return NativeOperationDone()
	})
	r := owner.core.Snapshot()
	require.Len(t, r.Attempts, 1)
	a := r.Attempts[0]
	require.Equal(t, "request-real", a.RequestID)
	require.Equal(t, "resp-real", a.ResponseID)
	require.Equal(t, "returned-model", a.ReturnedModel)
	require.Equal(t, model.SendSent, a.Send)
	require.Equal(t, model.TerminalCompleted, a.Terminal)
	require.True(t, a.EndOfStream)
	require.True(t, a.Closed)
	require.NotNil(t, a.Usage.Input)
	require.Zero(t, a.Usage.Input.Value, "measured zero must not become unknown")
	require.Nil(t, a.Usage.CacheRead, "absent fields must not become measured zero")
	require.Equal(t, model.UsageFinal, a.Usage.Confidence)
	require.EqualValues(t, len(wire), a.Delivery.BytesWritten)
	require.True(t, a.Delivery.HeadersCommitted)
	require.True(t, a.Delivery.BusinessCommitted)
	require.True(t, a.Delivery.FlushAttempted)
	require.True(t, a.Delivery.Flushed)
	require.False(t, a.Delivery.PossiblyCommitted)
	require.False(t, r.Billing.Durable, "receiving final usage is not a durable billing acknowledgment")
	require.EqualValues(t, 1, f.transport.sends.Load())
	require.Zero(t, f.transport.legacy.Load())
	require.ErrorIs(t, owner.core.AllowProtocolRecovery(f.ctx), execution.ErrOperationCommitted)
}

type nativeReceiptWriter struct {
	gin.ResponseWriter
	n             int
	err, flushErr error
}

func (w *nativeReceiptWriter) Write(p []byte) (int, error) {
	n := min(w.n, len(p))
	if n > 0 {
		_, _ = w.ResponseWriter.Write(p[:n])
	}
	return n, w.err
}
func (w *nativeReceiptWriter) FlushError() error { return w.flushErr }

func TestNativeHTTPReceiptRetainsPartialWriteAndFlushFailures(t *testing.T) {
	for _, name := range []string{"partial_error", "short_nil_error", "flush_error"} {
		t.Run(name, func(t *testing.T) {
			f, owner, c := nativeHTTPObservedFixture(t, "{}", "application/json")
			writeErr := errors.New("downstream refused write")
			underlying := &nativeReceiptWriter{ResponseWriter: c.Writer, n: 3, err: writeErr}
			if name == "short_nil_error" {
				underlying.err = nil
				writeErr = io.ErrShortWrite
			}
			if name == "flush_error" {
				underlying.n, underlying.err, underlying.flushErr = 100, nil, writeErr
			}
			c.Writer = underlying
			RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
				response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				c.Writer.Header().Set("Content-Type", "application/json")
				_, err = c.Writer.Write([]byte("business"))
				if name == "flush_error" {
					require.NoError(t, err)
					err = http.NewResponseController(c.Writer).Flush()
				}
				require.ErrorIs(t, err, writeErr)
				return NativeOperationStep{Err: err}
			})
			r := owner.core.Snapshot()
			require.Len(t, r.Attempts, 1)
			require.True(t, r.Delivery.BusinessCommitted)
			require.True(t, r.Delivery.Closed)
			require.False(t, r.Delivery.Flushed)
			require.Equal(t, name == "flush_error", r.Delivery.FlushAttempted)
			if name != "flush_error" {
				require.EqualValues(t, 3, r.Delivery.BytesWritten)
				require.True(t, r.Delivery.PossiblyCommitted)
			}
			require.ErrorIs(t, r.Failure, writeErr)
		})
	}
}

func TestNativeHTTPKeepaliveDoesNotCommitButWrittenIdentityDoes(t *testing.T) {
	f, owner, c := nativeHTTPObservedFixture(t, "{}", "application/json")
	RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
		response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		_, err = c.Writer.Write([]byte(": keepalive\n\n"))
		require.NoError(t, err)
		require.False(t, owner.core.Committed())
		_, err = c.Writer.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-real\"}}\n\n"))
		require.NoError(t, err)
		require.True(t, owner.core.Committed(), "a written, nonreplaceable identity blocks another generation")
		return NativeOperationDone()
	})
}

func TestNativeHTTPDecoderPreservesUnknownPartialAndMeasuredZero(t *testing.T) {
	for _, test := range []struct {
		name, wire string
		confidence model.UsageConfidence
		terminal   model.TerminalState
		usage      bool
	}{
		{"unknown_eof", "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\ndata: [DONE]\n\n", model.UsageUnknown, model.TerminalUnknown, false},
		{"partial_eof", "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}\n\n", model.UsagePartial, model.TerminalUnknown, true},
		{"confirmed_zero", "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}\n\ndata: invalid-extension\n\n", model.UsageFinal, model.TerminalCompleted, true},
		{"event_named_terminal", "event: response.completed\r\ndata: {\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}\r\n\r\n", model.UsageFinal, model.TerminalCompleted, true},
		{"failed_with_final_snapshot", "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}\n\n", model.UsageFinal, model.TerminalFailed, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, owner, _ := nativeHTTPObservedFixture(t, test.wire, "text/event-stream")
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			wire, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, test.wire, string(wire))
			require.NoError(t, response.Body.Close())
			a := owner.core.Snapshot().Attempts[0]
			require.Equal(t, "r", a.ResponseID)
			require.Equal(t, test.terminal, a.Terminal)
			require.True(t, a.EndOfStream)
			if test.usage {
				require.NotNil(t, a.Usage)
				require.Equal(t, test.confidence, a.Usage.Confidence)
				require.NotNil(t, a.Usage.Input)
				require.Zero(t, a.Usage.Input.Value)
				require.Nil(t, a.Usage.Output)
			} else {
				require.Nil(t, a.Usage)
			}
		})
	}
}

func TestNativeHTTPNextHonorsCancellationWithoutReading(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("owned lease lost")
	cancel(cause)
	s := &nativeOperationStream{}
	frame, fact, err := s.Next(ctx)
	require.ErrorIs(t, err, cause)
	require.Empty(t, frame.Data)
	require.Nil(t, fact.Usage)
}

func TestNativeHTTPWrittenTerminalCannotStartAnotherGeneration(t *testing.T) {
	for _, wire := range []string{
		"data: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"code\":\"server_error\",\"message\":\"retryable upstream failure\"}}\n\n",
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\"}}}\n\n",
	} {
		f, owner, c := nativeHTTPObservedFixture(t, "", "application/json")
		cycles := 0
		err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
			cycles++
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			c.Writer.Header().Set("Content-Type", "text/event-stream")
			_, err = c.Writer.Write([]byte(wire))
			require.NoError(t, err)
			return NativeOperationContinue()
		})
		require.ErrorIs(t, err, execution.ErrOperationCommitted)
		require.Equal(t, 1, cycles)
		require.EqualValues(t, 1, f.transport.sends.Load())
		require.True(t, owner.core.Snapshot().Delivery.BusinessCommitted)
	}
}

func TestNativeHTTPRawChatFactsFollowExistingTerminalContract(t *testing.T) {
	for _, sample := range []struct {
		name, wire string
		terminal   model.TerminalState
		confidence model.UsageConfidence
	}{
		{"usage_only_trailer_at_eof", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n", model.TerminalCompleted, model.UsageFinal},
		{"every_choice_finished", "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"},{\"index\":1,\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\n", model.TerminalCompleted, model.UsageFinal},
		{"unfinished_choice", "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"},{\"index\":1,\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\n", model.TerminalUnknown, model.UsagePartial},
		{"error_before_done", "data: {\"choices\":[{\"index\":0}],\"usage\":{\"prompt_tokens\":5}}\n\ndata: {\"error\":{\"message\":\"failed\"}}\n\ndata: [DONE]\n\n", model.TerminalFailed, model.UsagePartial},
	} {
		t.Run(sample.name, func(t *testing.T) {
			f, owner, _ := nativeHTTPObservedFixture(t, sample.wire, "text/event-stream", nativeUpstreamChat)
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			wire, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, sample.wire, string(wire))
			require.NoError(t, response.Body.Close())
			attempt := owner.core.Snapshot().Attempts[0]
			require.Equal(t, sample.terminal, attempt.Terminal)
			require.NotNil(t, attempt.Usage)
			require.Equal(t, sample.confidence, attempt.Usage.Confidence)
		})
	}
}

func TestNativeHTTPDecoderDoesNotPromoteUnframedTerminalAtEOF(t *testing.T) {
	wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}\n"
	f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream")
	response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, wire, string(body))
	require.NoError(t, response.Body.Close())
	attempt := owner.core.Snapshot().Attempts[0]
	require.True(t, attempt.EndOfStream)
	require.Equal(t, model.TerminalUnknown, attempt.Terminal)
	require.Nil(t, attempt.Usage)
}

func TestNativeHTTPDecoderRecognizesActualCompatibilityTerminals(t *testing.T) {
	for _, sample := range []struct{ name, contentType, wire string }{
		{"chat_chunks_without_object", "text/event-stream", "data: {\"id\":\"chat-r\",\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"},
		{"anthropic_json", "application/json", "{\"id\":\"msg-r\",\"type\":\"message\",\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			family := nativeUpstreamChat
			if sample.name == "anthropic_json" {
				family = nativeUpstreamAnthropic
			}
			f, owner, _ := nativeHTTPObservedFixture(t, sample.wire, sample.contentType, family)
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, sample.wire, string(body))
			require.NoError(t, response.Body.Close())
			attempt := owner.core.Snapshot().Attempts[0]
			require.Equal(t, model.TerminalCompleted, attempt.Terminal)
			require.NotNil(t, attempt.Usage)
			require.Equal(t, model.UsageFinal, attempt.Usage.Confidence)
			require.NotNil(t, attempt.Usage.Input)
			require.Zero(t, attempt.Usage.Input.Value)
		})
	}
}
