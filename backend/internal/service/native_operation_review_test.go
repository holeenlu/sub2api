package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNativeHTTPReviewResponseOwnerIndependentOfReadBoundaries(t *testing.T) {
	wire := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_owner\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_foreign\",\"usage\":{\"input_tokens\":900}}}\n\n"
	for _, singleByte := range []bool{false, true} {
		t.Run(map[bool]string{false: "one_read", true: "one_byte_reads"}[singleByte], func(t *testing.T) {
			f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream")
			if singleByte {
				f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(iotest.OneByteReader(strings.NewReader(wire)))}, nil
				}
			}
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			delivered, err := io.ReadAll(response.Body)
			require.ErrorIs(t, err, execution.ErrOperationFacts, "one transport read cannot bypass response ownership")
			if !singleByte {
				require.Empty(t, delivered, "facts from this read are checked before any of its wire is exposed")
			}
			scanner := newOpenAISSEJSONDocumentScanner(bufio.NewScanner(strings.NewReader(string(delivered))))
			for scanner.Scan() {
				require.NotEqual(t, "resp_foreign", gjson.Get(scanner.Frame().data, "response.id").String(), "no complete foreign event reaches the converter")
			}
			_ = response.Body.Close()
			require.Equal(t, "resp_owner", owner.core.Snapshot().Attempts[0].ResponseID)
		})
	}
}

func TestNativeHTTPReviewChunkingPreservesFactsAndActualDelivery(t *testing.T) {
	wire := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n" +
		"data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}\n\n"
	var expected *model.UsageFact
	for _, singleByte := range []bool{false, true} {
		t.Run(map[bool]string{false: "one_read", true: "one_byte_reads"}[singleByte], func(t *testing.T) {
			f, owner, c := nativeHTTPObservedFixture(t, wire, "text/event-stream")
			f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
				var reader io.Reader = strings.NewReader(wire)
				if singleByte {
					reader = iotest.OneByteReader(reader)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(reader)}, nil
			}
			recorder := c.Writer
			err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
				response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
				require.NoError(t, err)
				c.Writer.Header().Set("Content-Type", "text/event-stream")
				n, err := io.Copy(c.Writer, response.Body)
				require.NoError(t, err)
				require.EqualValues(t, len(wire), n)
				require.NoError(t, response.Body.Close())
				require.NoError(t, http.NewResponseController(c.Writer).Flush())
				return NativeOperationDone()
			})
			require.NoError(t, err)
			attempt := owner.core.Snapshot().Attempts[0]
			require.Equal(t, "r", attempt.ResponseID)
			require.Equal(t, model.TerminalCompleted, attempt.Terminal)
			require.True(t, attempt.EndOfStream)
			require.EqualValues(t, len(wire), attempt.Delivery.BytesWritten)
			require.True(t, attempt.Delivery.Flushed)
			require.True(t, attempt.Delivery.BusinessCommitted)
			require.EqualValues(t, len(wire), recorder.Size())
			if expected == nil {
				expected = attempt.Usage
			} else {
				require.Equal(t, expected, attempt.Usage)
			}
		})
	}
}

type nativeReviewEOFReader struct {
	wire  string
	reads int
}

func (r *nativeReviewEOFReader) Read(p []byte) (int, error) {
	r.reads++
	return copy(p, r.wire), io.EOF
}
func (*nativeReviewEOFReader) Close() error { return nil }

func TestNativeHTTPReviewQueuedFactsPrecedeWireAndKeepActualEOF(t *testing.T) {
	for _, concatenated := range []bool{false, true} {
		t.Run(map[bool]string{false: "separate_events", true: "concatenated_documents"}[concatenated], func(t *testing.T) {
			first := "{\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}"
			last := "{\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}"
			wire := "data: " + first + "\n\ndata: " + last + "\n\n"
			if concatenated {
				wire = "data: " + first + last + "\n\n"
			}
			reader := &nativeReviewEOFReader{wire: wire}
			s := &nativeOperationStream{body: reader, decode: nativeOperationDecoder{kind: nativeUpstreamResponses, sse: true, lineLimit: 1024, limit: 1024}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			frame, fact, err := s.Next(ctx)
			require.NoError(t, err)
			require.Empty(t, frame.Data)
			require.Equal(t, "r", fact.ResponseID)
			require.Equal(t, model.TerminalUnknown, fact.Terminal)
			cancel()
			frame, fact, err = s.Next(ctx)
			require.ErrorIs(t, err, io.EOF)
			require.Equal(t, wire, string(frame.Data))
			require.Equal(t, model.TerminalCompleted, fact.Terminal)
			require.Equal(t, model.UsageFinal, fact.Usage.Confidence)
			require.Equal(t, 1, reader.reads, "queued facts survive cancellation without another upstream read")
			require.Empty(t, s.facts)
			require.Nil(t, s.wire)
			_, _, err = s.Next(ctx)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 1, reader.reads)
			require.NoError(t, s.Close())
		})
	}
}

func TestNativeHTTPReviewExtensionsAndTerminalTailsAreNotSnapshots(t *testing.T) {
	for _, extension := range []string{
		"{\"type\":\"vendor.telemetry\",\"response\":{\"id\":\"foreign\",\"status\":\"completed\",\"usage\":{\"input_tokens\":900}}}",
		"{\"type\":\"response.future_telemetry\",\"usage\":{\"input_tokens\":900}}",
		"{\"type\":\"response.output_text.delta\",\"delta\":\"x\",\"usage\":{\"input_tokens\":900}}",
	} {
		d := nativeOperationDecoder{sse: true}
		require.Nil(t, d.payload([]byte(extension)).Usage)
		initial := d.payload([]byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1}}}"))
		require.EqualValues(t, 1, initial.Usage.Input.Value)
		d.payload([]byte(extension))
		tail := d.payload([]byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":900}}}"))
		require.Nil(t, tail.Usage)
		require.Equal(t, initial.Usage, d.facts().Usage)
	}
}

func TestNativeHTTPReviewSSEEnvelopeUsesSharedPrecedence(t *testing.T) {
	for _, sample := range []struct {
		name, eventLines, payload, effective string
		terminal                             model.TerminalState
		confidence                           model.UsageConfidence
	}{
		{"named_without_json_type", "event: response.completed\n", "{\"response\":{\"id\":\"r\",\"status\":\"completed\",\"usage\":{\"input_tokens\":0}}}", "response.completed", model.TerminalCompleted, model.UsageFinal},
		{"matching", "event: response.completed\n", "{\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}", "response.completed", model.TerminalCompleted, model.UsageFinal},
		{"payload_failure_wins", "event: response.completed\n", "{\"type\":\"response.failed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}", "response.failed", model.TerminalFailed, model.UsageFinal},
		{"payload_progress_does_not_finish", "event: response.completed\n", "{\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}", "response.in_progress", model.TerminalUnknown, model.UsagePartial},
		{"payload_extension_does_not_finish", "event: response.completed\n", "{\"type\":\"vendor.telemetry\",\"usage\":{\"input_tokens\":900}}", "vendor.telemetry", model.TerminalUnknown, model.UsageUnknown},
		{"whitespace_json_type_uses_event", "event: response.completed\n", "{\"type\":\"  \",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}", "response.completed", model.TerminalCompleted, model.UsageFinal},
		{"last_event_name_wins", "event: response.completed\nevent: response.failed\n", "{\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}", "response.failed", model.TerminalFailed, model.UsageFinal},
		{"event_name_does_not_invent_usage", "event: response.completed\n", "{\"response\":{\"id\":\"r\",\"status\":\"completed\"}}", "response.completed", model.TerminalCompleted, model.UsageUnknown},
		{"indented_existing_parser_tolerance", "  event: response.completed\n", "{\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}", "response.completed", model.TerminalCompleted, model.UsageFinal},
	} {
		t.Run(sample.name, func(t *testing.T) {
			wire := sample.eventLines + "data: " + sample.payload + "\n\n"
			scanner := newOpenAISSEJSONDocumentScanner(bufio.NewScanner(strings.NewReader(wire)))
			require.True(t, scanner.Scan())
			require.Equal(t, sample.effective, scanner.Frame().eventType, "compare against the actual consumer framing rule")
			for _, singleByte := range []bool{false, true} {
				t.Run(map[bool]string{false: "one_read", true: "one_byte_reads"}[singleByte], func(t *testing.T) {
					f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream")
					if singleByte {
						f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
							return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(iotest.OneByteReader(strings.NewReader(wire)))}, nil
						}
					}
					response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
					require.NoError(t, err)
					actual, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.Equal(t, wire, string(actual))
					require.NoError(t, response.Body.Close())
					attempt := owner.core.Snapshot().Attempts[0]
					require.Equal(t, sample.terminal, attempt.Terminal)
					if sample.confidence == model.UsageUnknown {
						require.Nil(t, attempt.Usage)
					} else {
						require.Equal(t, sample.confidence, attempt.Usage.Confidence)
						require.NotNil(t, attempt.Usage.Input)
						require.Zero(t, attempt.Usage.Input.Value)
					}
				})
			}
		})
	}
}

func TestNativeHTTPReviewFrameMetadataUsesExistingLimit(t *testing.T) {
	wire := ":" + strings.Repeat("x", 40) + "\n:" + strings.Repeat("x", 40) + "\n" +
		"event: response.completed\ndata: {\"response\":{\"usage\":{\"input_tokens\":1}}}\n\n"
	d := nativeOperationDecoder{kind: nativeUpstreamResponses, sse: true, lineLimit: 64, limit: 1024}
	for _, fact := range d.feed([]byte(wire), true) {
		require.Nil(t, fact.Usage)
		require.Equal(t, model.TerminalUnknown, fact.Terminal)
	}
	require.Nil(t, d.usage, "metadata and comments count toward the existing event bound")
}

func TestNativeHTTPReviewUnknownExtensionCannotBecomeUsage(t *testing.T) {
	wire := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_owner\"}}\n\n" +
		"data: {\"type\":\"vendor.telemetry\",\"usage\":{\"output_tokens\":999},\"extension\":{\"future\":true}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_owner\",\"usage\":{\"input_tokens\":1}}}\n\n"
	f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream")
	response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
	require.NoError(t, err)
	actual, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, wire, string(actual), "unrecognized extensions remain transparent wire")
	require.NoError(t, response.Body.Close())
	u := owner.core.Snapshot().Attempts[0].Usage
	require.NotNil(t, u)
	require.EqualValues(t, 1, u.Input.Value)
	require.Nil(t, u.Output, "extension keys do not establish protocol usage")
}

func TestNativeHTTPReviewTerminalTailCannotReplaceUsage(t *testing.T) {
	wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_owner\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n\n" +
		"data: {\"type\":\"vendor.telemetry\",\"usage\":{\"input_tokens\":900}}\n\n"
	f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream")
	response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
	require.NoError(t, err)
	actual, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, wire, string(actual))
	require.NoError(t, response.Body.Close())
	u := owner.core.Snapshot().Attempts[0].Usage
	require.EqualValues(t, 1, u.Input.Value)
	require.EqualValues(t, 2, u.Output.Value)
}

func TestNativeHTTPReviewCacheAliasesKeepObservedConsumption(t *testing.T) {
	for _, sample := range []struct {
		name, usage string
		read, write int64
	}{
		{"cached_tokens", "{\"cached_tokens\":3}", 3, 0},
		{"cache_read_tokens", "{\"cache_read_tokens\":4}", 4, 0},
		{"cache_write_input_tokens", "{\"cache_write_input_tokens\":5}", 0, 5},
		{"cache_creation_tokens", "{\"cache_creation_tokens\":6}", 0, 6},
		{"prompt_cache_creation", "{\"prompt_tokens_details\":{\"cache_creation_tokens\":7}}", 0, 7},
	} {
		t.Run(sample.name, func(t *testing.T) {
			body := "{\"object\":\"response\",\"status\":\"completed\",\"usage\":" + sample.usage + "}"
			legacy, known := extractOpenAIUsageFromJSONBytes([]byte(body))
			require.True(t, known)
			require.True(t, legacy.HasTokens(), "existing consumer already recognizes these measurements")
			f, owner, _ := nativeHTTPObservedFixture(t, body, "application/json")
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			_, err = io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			u := owner.core.Snapshot().Attempts[0].Usage
			require.NotNil(t, u, "Native facts cannot discard consumption known to the real converter")
			if sample.read > 0 {
				require.NotNil(t, u.CacheRead)
				require.Equal(t, sample.read, u.CacheRead.Value)
			} else {
				require.NotNil(t, u.CacheWrite)
				require.Equal(t, sample.write, u.CacheWrite.Value)
			}
			require.Nil(t, u.Input)
			require.Nil(t, u.Output)
		})
	}
}

func TestNativeHTTPReviewRequestExtensionsReachTheSealedSend(t *testing.T) {
	body := []byte("{\"model\":\"client-model\",\"input\":\"native\",\"messages\":[{\"content\":42}],\"stream\":false,\"vendor_extension\":{\"number\":9007199254740993,\"nested\":{\"id\":\"retain-me\"}}}")
	f := newNativeOperationFixtureWithInput(t, true, model.ProtocolResponses, body, 1)
	f.resource.Credentials["base_url"] = "https://api.openai.com/v1"
	f.resource.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.1"}
	var sent []byte
	f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
		var err error
		sent, err = io.ReadAll(req.Body)
		require.NoError(t, err)
		return openAICompatSSECompletedResponse("resp_extension", "gpt-5.1"), nil
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body))).WithContext(f.ctx)
	c.Set("api_key", f.key)
	err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
		_, err := TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) { return f.service.Forward(f.ctx, c, f.resource, body) })
		return NativeOperationStep{Err: err}
	})
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", gjson.GetBytes(sent, "vendor_extension.number").Raw)
	require.Equal(t, "retain-me", gjson.GetBytes(sent, "vendor_extension.nested.id").String())
	require.Equal(t, "native", gjson.GetBytes(sent, "input").String())
	require.False(t, gjson.GetBytes(sent, "messages").Exists())
	require.Equal(t, sha256.Sum256(body), nativeHTTPFromContext(f.ctx).core.Snapshot().OriginalWireDigest)
	require.EqualValues(t, 1, f.transport.sends.Load())
	require.Zero(t, f.transport.legacy.Load())
}

func TestNativeHTTPReviewTerminalUsageAndBusinessOutcomeAreIndependent(t *testing.T) {
	for _, sample := range []struct {
		name, prefix, event string
		terminal            model.TerminalState
		confidence          model.UsageConfidence
	}{
		{"failed_final", "", "{\"type\":\"response.failed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}", model.TerminalFailed, model.UsageFinal},
		{"incomplete_final", "", "{\"type\":\"response.incomplete\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}", model.TerminalIncomplete, model.UsageFinal},
		{"canceled_final", "", "{\"type\":\"response.canceled\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}", model.TerminalCanceled, model.UsageFinal},
		{"cancelled_final", "", "{\"type\":\"response.cancelled\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}", model.TerminalCanceled, model.UsageFinal},
		{"bare_error_pending", "", "{\"type\":\"error\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}", model.TerminalUnknown, model.UsagePartial},
		{"earlier_usage_then_completed", "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}\n\n", "{\"type\":\"response.completed\",\"response\":{\"id\":\"r\"}}", model.TerminalCompleted, model.UsagePartial},
		{"earlier_usage_then_failed", "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}\n\n", "{\"type\":\"response.failed\",\"response\":{\"id\":\"r\"}}", model.TerminalFailed, model.UsagePartial},
		{"terminal_omits_observed_counter", "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0,\"output_tokens\":2}}}\n\n", "{\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":0}}}", model.TerminalCompleted, model.UsagePartial},
	} {
		t.Run(sample.name, func(t *testing.T) {
			wire := sample.prefix + "data: " + sample.event + "\n\n"
			f, owner, _ := nativeHTTPObservedFixture(t, wire, "text/event-stream")
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			actual, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, wire, string(actual))
			require.NoError(t, response.Body.Close())
			fact := owner.core.Snapshot().Attempts[0]
			require.Equal(t, sample.terminal, fact.Terminal)
			require.NotNil(t, fact.Usage)
			require.Equal(t, sample.confidence, fact.Usage.Confidence)
			require.Equal(t, sample.terminal != model.TerminalUnknown, fact.Usage.TerminalConfirmed)
			require.NotNil(t, fact.Usage.Input)
			require.EqualValues(t, 0, fact.Usage.Input.Value)
			require.NotNil(t, fact.Usage.Output)
			require.EqualValues(t, 2, fact.Usage.Output.Value)
		})
	}
}
