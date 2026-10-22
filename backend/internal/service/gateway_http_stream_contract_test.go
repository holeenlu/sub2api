//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

func httpStreamContractContext() (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	return c, rec
}
func httpStreamContractResponse(wire string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
}

const httpStreamContractPartial = "data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
const httpStreamContractDone = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

func TestHTTPStreamContractChatFallbackMustNotInventSuccess(t *testing.T) {
	for _, protocol := range []string{"responses", "messages"} {
		for _, ending := range []string{"clean_eof", "error_event"} {
			t.Run(protocol+"/"+ending, func(t *testing.T) {
				c, rec := httpStreamContractContext()
				wire := httpStreamContractPartial
				if ending == "error_event" {
					wire += "data: {\"error\":{\"type\":\"server_error\",\"message\":\"backend failed\"}}\n\n"
				}
				svc := &OpenAIGatewayService{}
				var err error
				terminal := "response.completed"
				if protocol == "responses" {
					_, err = svc.streamChatCompletionsAsResponses(c, httpStreamContractResponse(wire), "model", nil, nil, false, nil, "model", "model", nil, nil, time.Now())
				} else {
					terminal = "message_stop"
					_, err = svc.streamChatCompletionsAsAnthropic(c, httpStreamContractResponse(wire), "model", "model", "model", nil, nil, time.Now())
				}
				t.Logf("err=%v synthetic_terminal=%v", err, strings.Contains(rec.Body.String(), terminal))
				if err == nil || strings.Contains(rec.Body.String(), terminal) {
					t.Error("upstream had no success terminal; the bridge must return failure without inventing success")
				}
			})
		}
	}
}

func TestHTTPStreamContractClaudeTerminalNeedsStart(t *testing.T) {
	for _, mode := range []string{"native", "passthrough", "compat_control"} {
		t.Run(mode, func(t *testing.T) {
			c, rec := httpStreamContractContext()
			resp := httpStreamContractResponse("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			svc := newSSEStreamingGatewayService()
			var err error
			switch mode {
			case "native":
				_, err = svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
			case "passthrough":
				_, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
			default:
				_, err = svc.readAnthropicSSEStream(resp, c, time.Now(), nil, func(sseEvent, bool) (bool, error) { return false, nil })
			}
			t.Logf("err=%v downstream_bytes=%d", err, rec.Body.Len())
			if err == nil {
				t.Error("message_stop without message_start must not succeed")
			}
		})
	}
}

func TestHTTPStreamContractRawUsageIsNotProofOfCompletion(t *testing.T) {
	c, _ := httpStreamContractContext()
	svc := &OpenAIGatewayService{}
	wire := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":1}}\n\n"
	result, err := svc.streamRawChatCompletions(c, httpStreamContractResponse(wire), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "model", "model", "model", nil, nil, time.Now(), 0)
	t.Logf("err=%v usage=%+v", err, result.Usage)
	if err == nil {
		t.Error("usage attached to an unfinished content chunk must not turn EOF into success")
	}
}

func TestHTTPStreamContractRawPreoutputStagingLimit(t *testing.T) {
	c, rec := httpStreamContractContext()
	// Every individual frame is small; only the aggregate pending stream is large.
	prefix := strings.Repeat(":"+strings.Repeat("x", 1020)+"\n\n", 9*1024)
	svc := &OpenAIGatewayService{}
	_, err := svc.streamRawChatCompletions(c, httpStreamContractResponse(prefix+httpStreamContractPartial+httpStreamContractDone), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "model", "model", "model", nil, nil, time.Now(), 64*1024)
	t.Logf("err=%v metadata_before_first_semantic=%d downstream_bytes=%d", err, len(prefix), rec.Body.Len())
	if err == nil && rec.Body.Len() > 8*1024*1024 {
		t.Error("raw Chat pending metadata exceeds the gateway's existing 8 MiB pre-output staging budget")
	}
}

func TestHTTPStreamContractFallbackFlushFailureAttribution(t *testing.T) {
	for _, protocol := range []string{"responses", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			recorder := &flushFailureRecorder{ResponseRecorder: httptest.NewRecorder(), err: errors.New("local flush failure")}
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			finish := GuardSSEWrites(c)
			defer finish()
			reader, writer := io.Pipe()
			release := make(chan struct{})
			producerDone := make(chan struct{})
			go func() {
				defer close(producerDone)
				defer func() { _ = writer.Close() }()
				_, _ = io.WriteString(writer, httpStreamContractPartial)
				<-release
			}()
			resp := httpStreamContractResponse("")
			resp.Body = reader
			svc := &OpenAIGatewayService{}
			var result *OpenAIForwardResult
			var err error
			if protocol == "responses" {
				result, err = svc.streamChatCompletionsAsResponses(c, resp, "model", nil, nil, false, nil, "model", "model", nil, nil, time.Now())
			} else {
				result, err = svc.streamChatCompletionsAsAnthropic(c, resp, "model", "model", "model", nil, nil, time.Now())
			}
			close(release)
			<-producerDone
			if result == nil {
				t.Fatalf("missing result: %v", err)
			}
			t.Logf("err=%v downstream_failure=%v parent_context=%v ClientDisconnect=%v", err, downstreamStreamError(c), c.Request.Context().Err(), result.ClientDisconnect)
			if !result.ClientDisconnect {
				t.Error("local flush failure must not lose client-disconnect attribution when the bounded drain closes upstream")
			}
		})
	}
}

func TestHTTPStreamContractGeminiCompleteFrameParsing(t *testing.T) {
	payload := "{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":1}}"
	for _, tc := range []struct{ name, wire string }{
		{"lf_control", "data: " + payload + "\n\n"},
		{"multiline", "data: {\ndata: " + payload[1:] + "\n\n"},
		{"cr_only", "data: " + payload + "\r\r"},
		{"bom", "\ufeffdata: " + payload + "\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, usage, err := collectGeminiSSE(strings.NewReader(tc.wire), false)
			t.Logf("err=%v usage=%+v", err, usage)
			if err != nil || usage == nil || usage.InputTokens != 7 {
				t.Error("equivalent complete SSE frames must preserve Gemini completion and usage")
			}
		})
	}
}

func TestHTTPStreamContractClientToolAdapterCRFrames(t *testing.T) {
	for _, ending := range []string{"\n", "\r"} {
		t.Run(map[string]string{"\n": "lf_control", "\r": "cr_only"}[ending], func(t *testing.T) {
			wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"output\":[]}}" + ending + ending
			body := newResponsesClientToolStreamBody(io.NopCloser(strings.NewReader(wire)), apicompat.ResponsesClientToolMapping{CustomTools: map[string]bool{"apply_patch": true}}, 1024*1024)
			defer func() { _ = body.Close() }()
			output, err := io.ReadAll(body)
			t.Logf("err=%v output_bytes=%d", err, len(output))
			if err != nil || !strings.Contains(string(output), "response.completed") {
				t.Error("the tool adapter must preserve complete CR-only SSE frames")
			}
		})
	}
}

type httpStreamContractUpstream struct {
	HTTPUpstream
	body io.ReadCloser
}

func (u *httpStreamContractUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	stop := context.AfterFunc(req.Context(), func() { _ = u.body.Close() })
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: &openAIRequestContextReadCloser{ReadCloser: u.body, cleanup: func() { stop() }}}, nil
}
func TestHTTPStreamContractRawFullForwardFirstOutputBudget(t *testing.T) {
	c, rec := httpStreamContractContext()
	budget := &OpenAITurnBudget{deadline: time.Now().Add(60 * time.Millisecond), limit: 8}
	c.Request = c.Request.WithContext(WithOpenAITurnBudget(c.Request.Context(), budget))
	reader, writer := io.Pipe()
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, ": heartbeat\n\n")
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(writer, httpStreamContractPartial+httpStreamContractDone)
	}()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpStreamContractUpstream{body: reader}}
	started := time.Now()
	_, err := svc.forwardAsRawChatCompletions(c.Request.Context(), c, rawChatCompletionsTestAccount(), largeRawChatCompletionsBody(), "")
	<-producerDone
	t.Logf("err=%v elapsed=%s attempts=%d bytes=%d", err, time.Since(started), budget.attempts, rec.Body.Len())
	if err == nil {
		t.Error("the full raw Chat forwarding route returned success after its absolute first-output deadline")
	}
}

// First-output expiry applies to metadata-only streams, not a generation that
// already emitted content. The upstream double observes request cancellation.
func TestHTTPStreamContractRawDeadlineEndsAtContent(t *testing.T) {
	for _, content := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty_delta", true: "long_generation"}[content], func(t *testing.T) {
			c, _ := httpStreamContractContext()
			started := time.Now()
			c.Request = c.Request.WithContext(WithOpenAITurnBudget(c.Request.Context(), &OpenAITurnBudget{started: started, deadline: started.Add(150 * time.Millisecond), limit: 8}))
			reader, writer := io.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { _ = writer.Close() }()
				first := "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n"
				if content {
					first = httpStreamContractPartial
				}
				_, _ = io.WriteString(writer, first)
				time.Sleep(250 * time.Millisecond)
				_, _ = io.WriteString(writer, httpStreamContractDone)
			}()
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpStreamContractUpstream{body: reader}}
			_, err := svc.forwardAsRawChatCompletions(c.Request.Context(), c, rawChatCompletionsTestAccount(), largeRawChatCompletionsBody(), "")
			<-done
			if content && err != nil {
				t.Fatalf("first content must release the deadline: %v", err)
			}
			if !content && !errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) {
				t.Fatalf("empty deltas must not release the deadline: %v", err)
			}
		})
	}
}

type httpStreamHeaderWait struct{ HTTPUpstream }

func (*httpStreamHeaderWait) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}
func TestHTTPStreamContractRawHeaderWaitBudget(t *testing.T) {
	c, _ := httpStreamContractContext()
	started := time.Now()
	c.Request = c.Request.WithContext(WithOpenAITurnBudget(c.Request.Context(), &OpenAITurnBudget{started: started, deadline: started.Add(50 * time.Millisecond), limit: 8}))
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpStreamHeaderWait{}}
	_, err := svc.forwardAsRawChatCompletions(c.Request.Context(), c, rawChatCompletionsTestAccount(), largeRawChatCompletionsBody(), "")
	if !errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) {
		t.Fatalf("header wait must obey the same deadline: %v", err)
	}
}

type httpStreamFlushCounter struct {
	*httptest.ResponseRecorder
	flushes int
}

func (w *httpStreamFlushCounter) Flush() { w.flushes++; w.ResponseRecorder.Flush() }
func TestHTTPStreamContractRawFlushesAtEventBoundary(t *testing.T) {
	rec := &httpStreamFlushCounter{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	svc := &OpenAIGatewayService{}
	_, err := svc.streamRawChatCompletions(c, httpStreamContractResponse(httpStreamContractPartial+httpStreamContractDone), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "model", "model", "model", nil, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if rec.flushes != 3 {
		t.Fatalf("three complete frames should flush three times, got %d", rec.flushes)
	}
}

func TestHTTPStreamContractFallbackFirstOutputBudget(t *testing.T) {
	for _, protocol := range []string{"responses", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			c, _ := httpStreamContractContext()
			start := time.Now()
			c.Request = c.Request.WithContext(WithOpenAITurnBudget(c.Request.Context(), &OpenAITurnBudget{started: start, deadline: start.Add(50 * time.Millisecond)}))
			reader, writer := io.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { _ = writer.Close() }()
				_, _ = io.WriteString(writer, ": ping\n\n")
				time.Sleep(100 * time.Millisecond)
				_, _ = io.WriteString(writer, httpStreamContractPartial+httpStreamContractDone)
			}()
			resp := httpStreamContractResponse("")
			resp.Body = reader
			svc := &OpenAIGatewayService{}
			var err error
			if protocol == "responses" {
				_, err = svc.streamChatCompletionsAsResponses(c, resp, "model", nil, nil, false, nil, "model", "model", nil, nil, start)
			} else {
				_, err = svc.streamChatCompletionsAsAnthropic(c, resp, "model", "model", "model", nil, nil, start)
			}
			<-done
			if !errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) {
				t.Fatalf("compatibility read must keep the shared deadline: %v", err)
			}
		})
	}
}

func TestHTTPStreamContractCanceledPreoutputStillDrainsUsage(t *testing.T) {
	c, _ := httpStreamContractContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	start := time.Now()
	ctx = WithOpenAITurnBudget(ctx, &OpenAITurnBudget{started: start, deadline: start.Add(30 * time.Millisecond)})
	c.Request = c.Request.WithContext(ctx)
	reader, writer := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, ": ping\n\n")
		cancel()
		time.Sleep(70 * time.Millisecond)
		_, _ = io.WriteString(writer, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	}()
	resp := httpStreamContractResponse("")
	resp.Body = reader
	svc := &OpenAIGatewayService{}
	result, err := svc.streamRawChatCompletions(c, resp, &Account{ID: 1, Platform: PlatformOpenAI}, "model", "model", "model", nil, nil, start, 64*1024)
	<-done
	if err != nil || result == nil || !result.ClientDisconnect || result.Usage.InputTokens != 7 {
		t.Fatalf("cancellation must retain bounded final usage, result=%+v err=%v", result, err)
	}
}

type httpBoundaryEndingReader struct {
	err         error
	beforeError func()
}

func (r httpBoundaryEndingReader) Read([]byte) (int, error) {
	if r.beforeError != nil {
		r.beforeError()
	}
	return 0, r.err
}

func TestHTTPBoundaryUpstreamCancellationIsNotClientCancellation(t *testing.T) {
	for _, reason := range []struct {
		name string
		err  error
	}{{"deadline", context.DeadlineExceeded}, {"canceled", context.Canceled}} {
		for _, mode := range []string{"raw", "claude_native", "claude_passthrough", "responses_control"} {
			t.Run(reason.name+"/"+mode, func(t *testing.T) {
				c, rec := httpStreamContractContext()
				prefix := httpStreamContractPartial
				if strings.HasPrefix(mode, "claude") {
					prefix = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\n"
				}
				resp := httpStreamContractResponse("")
				resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(prefix), httpBoundaryEndingReader{err: reason.err}))
				var err error
				disconnected := false
				if mode == "raw" {
					result, e := (&OpenAIGatewayService{}).streamRawChatCompletions(c, resp, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "model", "model", "model", nil, nil, time.Now(), 0)
					err = e
					if result != nil {
						disconnected = result.ClientDisconnect
					}
				} else if mode == "responses_control" {
					result, e := (&OpenAIGatewayService{}).streamChatCompletionsAsResponses(c, resp, "model", nil, nil, false, nil, "model", "model", nil, nil, time.Now())
					err = e
					if result != nil {
						disconnected = result.ClientDisconnect
					}
				} else {
					svc := newSSEStreamingGatewayService()
					var result *streamingResult
					if mode == "claude_native" {
						result, err = svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
					} else {
						result, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
					}
					if result != nil {
						disconnected = result.clientDisconnect
					}
				}
				t.Logf("client_context=%v err=%v client_disconnect=%v written=%d", c.Request.Context().Err(), err, disconnected, rec.Body.Len())
				if err == nil || disconnected || c.Request.Context().Err() != nil {
					t.Error("a live downstream must see upstream cancellation as an upstream failure")
				}
			})
		}
	}
}

func TestHTTPBoundaryCommentOnlySSEHasNoSuccessTerminal(t *testing.T) {
	for _, wire := range []string{": ping\n\n", "id: keepalive\n\n", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"} {
		t.Run(wire, func(t *testing.T) {
			c, rec := httpStreamContractContext()
			result, err := (&OpenAIGatewayService{}).streamRawChatCompletions(c, httpStreamContractResponse(wire), &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "model", "model", "model", nil, nil, time.Now(), 0)
			t.Logf("error=%v result=%v bytes=%d", err, result != nil, rec.Body.Len())
			if err == nil {
				t.Error("text/event-stream with metadata only is not a completed generation")
			}
		})
	}
}

type httpBoundaryObservedBody struct {
	*io.PipeReader
	started chan struct{}
	once    sync.Once
}

func (r *httpBoundaryObservedBody) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.PipeReader.Read(p)
}

type httpBoundaryUpstream struct {
	HTTPUpstream
	response *http.Response
}

func (u *httpBoundaryUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return u.response, nil
}

// The shared sender already cancels its context; these deliberately context-blind
// bodies verify the explicit Close fallback for plugin/custom transports.
func TestHTTPBoundaryCanceledBufferedReadsAreBounded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream bool
		status int
	}{{"stream_success_control", true, 200}, {"stream_error_body", true, 503}, {"nonstream_success_body", false, 200}, {"nonstream_error_body", false, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader, writer := io.Pipe()
			defer func() { _ = writer.Close() }()
			body := &httpBoundaryObservedBody{PipeReader: reader, started: make(chan struct{})}
			resp := httpStreamContractResponse("")
			resp.StatusCode = tc.status
			resp.Body = body
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpBoundaryUpstream{response: resp}}
			c, _ := httpStreamContractContext()
			c.Request = c.Request.WithContext(ctx)
			request := "{\"model\":\"deepseek-v4-pro\",\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}],\"stream\":false}"
			if tc.stream {
				request = strings.Replace(request, "\"stream\":false", "\"stream\":true", 1)
			}
			done := make(chan error, 1)
			go func() {
				_, err := svc.forwardAsRawChatCompletions(ctx, c, rawChatCompletionsTestAccount(), []byte(request), "")
				done <- err
			}()
			select {
			case <-body.started:
			case err := <-done:
				t.Fatalf("did not reach body read: %v", err)
			case <-time.After(time.Second):
				_ = reader.Close()
				t.Fatal("body not read")
			}
			start := time.Now()
			cancel()
			select {
			case err := <-done:
				t.Logf("returned_after_cancel=%s err=%v", time.Since(start), err)
			case <-time.After(openAIWSUsageDrainTimeout + 350*time.Millisecond):
				_ = reader.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("manual close did not stop request")
				}
				t.Errorf("canceled caller still blocked after %s; only manual upstream close released it", time.Since(start))
			}
		})
	}
}

func TestHTTPBoundaryRawFirstTokenMustExcludeRoleMetadata(t *testing.T) {
	for _, mode := range []string{"raw", "responses_control"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := httpStreamContractContext()
			reader, writer := io.Pipe()
			resp := httpStreamContractResponse("")
			resp.Body = reader
			start := time.Now()
			textAt := make(chan time.Duration, 1)
			go func() {
				defer func() { _ = writer.Close() }()
				_, _ = io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
				time.Sleep(90 * time.Millisecond)
				textAt <- time.Since(start)
				_, _ = io.WriteString(writer, httpStreamContractPartial+httpStreamContractDone)
			}()
			svc := &OpenAIGatewayService{}
			var result *OpenAIForwardResult
			var err error
			if mode == "raw" {
				result, err = svc.streamRawChatCompletions(c, resp, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "model", "model", "model", nil, nil, start, 0)
			} else {
				result, err = svc.streamChatCompletionsAsResponses(c, resp, "model", nil, nil, false, nil, "model", "model", nil, nil, start)
			}
			sent := <-textAt
			if err != nil || result == nil || result.FirstTokenMs == nil {
				t.Fatalf("missing result: %v", err)
			}
			t.Logf("first_token_ms=%d first_text_sent_ms=%d", *result.FirstTokenMs, sent.Milliseconds())
			if time.Duration(*result.FirstTokenMs)*time.Millisecond+15*time.Millisecond < sent {
				t.Error("first_token_ms measures metadata instead of semantic output")
			}
		})
	}
}

type httpBoundaryCompleteBody struct {
	io.ReadCloser
	completed bool
}

func (r *httpBoundaryCompleteBody) MarkComplete() { r.completed = true }
func TestHTTPBoundarySuccessfulCompatibilityTerminalReportsCompletion(t *testing.T) {
	for _, tc := range []struct{ name, ending string }{{"done_control", httpStreamContractDone}, {"finish_reason", "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"}, {"usage_trailer", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":1}}\n\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := httpStreamContractContext()
			resp := httpStreamContractResponse(httpStreamContractPartial + tc.ending)
			body := &httpBoundaryCompleteBody{ReadCloser: resp.Body}
			resp.Body = body
			_, err := (&OpenAIGatewayService{}).streamRawChatCompletions(c, resp, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "model", "model", "model", nil, nil, time.Now(), 0)
			t.Logf("err=%v transport_completion_reported=%v", err, body.completed)
			if err != nil || !body.completed {
				t.Error("an accepted success must consistently report transport completion")
			}
		})
	}
}

func TestHTTPBoundaryErrorBodyRetainsFirstOutputBudget(t *testing.T) {
	budget := NewOpenAITurnBudget(nil)
	budget.deadline = time.Now().Add(70 * time.Millisecond)
	ctx := WithOpenAITurnBudget(context.Background(), budget)
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	body := &httpBoundaryObservedBody{PipeReader: reader, started: make(chan struct{})}
	resp := httpStreamContractResponse("")
	resp.StatusCode = 503
	resp.Body = body
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpBoundaryUpstream{response: resp}}
	c, _ := httpStreamContractContext()
	c.Request = c.Request.WithContext(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := svc.forwardAsRawChatCompletions(ctx, c, rawChatCompletionsTestAccount(), []byte("{\"model\":\"deepseek-v4-pro\",\"messages\":[{\"role\":\"user\",\"content\":\"hello\"}],\"stream\":true}"), "")
		done <- err
	}()
	select {
	case <-body.started:
	case err := <-done:
		t.Fatalf("did not reach error body: %v", err)
	case <-time.After(time.Second):
		_ = reader.Close()
		t.Fatal("body not read")
	}
	select {
	case err := <-done:
		t.Logf("budget_error=%v", err)
		if !IsTurnBudgetError(err) {
			t.Errorf("expected budget failure: %v", err)
		}
	case <-time.After(160 * time.Millisecond):
		_ = reader.Close()
		select {
		case err := <-done:
			t.Logf("after_manual_close=%v", err)
		case <-time.After(time.Second):
			t.Fatal("manual close did not unblock request")
		}
		t.Error("70ms first-output deadline did not interrupt an unfinished HTTP 503 error body after 160ms")
	}
}

type httpBoundaryNetworkUpstream struct {
	HTTPUpstream
	client *http.Client
}

func (u *httpBoundaryNetworkUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}

func TestHTTPBoundaryNativeHTTPClientCancellation(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			stopped := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(stopped)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, _ := httpStreamContractContext()
			c.Request = c.Request.WithContext(ctx)
			svc := &OpenAIGatewayService{httpUpstream: &httpBoundaryNetworkUpstream{client: server.Client()}}
			resp, err := svc.sendCCUpstreamRequest(ctx, c, rawChatCompletionsTestAccount(), server.URL, []byte("{}"), false, "fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			cancel()
			start := time.Now()
			_, err = io.ReadAll(resp.Body)
			if err == nil {
				t.Fatal("cancellation must end the unfinished body")
			}
			if time.Since(start) > openAIWSUsageDrainTimeout+500*time.Millisecond {
				t.Fatal("native HTTP cancellation exceeded drain allowance")
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("upstream server did not observe request cancellation")
			}
		})
	}
}

func TestHTTPBoundaryBufferedBudgetKeepsCancellationDrain(t *testing.T) {
	for _, cancelClient := range []bool{false, true} {
		name := "live_client_budget"
		if cancelClient {
			name = "canceled_client_drain"
		}
		t.Run(name, func(t *testing.T) {
			budget := NewOpenAITurnBudget(nil)
			budget.deadline = time.Now().Add(70 * time.Millisecond)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := WithOpenAITurnBudget(parent, budget)
			c, rec := httpStreamContractContext()
			c.Request = c.Request.WithContext(ctx)
			reader, writer := io.Pipe()
			resp := httpStreamContractResponse("")
			resp.Body = reader
			svc := &OpenAIGatewayService{httpUpstream: &httpBoundaryUpstream{response: resp}}
			response, err := svc.sendCCUpstreamRequest(ctx, c, rawChatCompletionsTestAccount(), "https://upstream.invalid/v1/chat/completions", []byte("{}"), false, "fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()
			sent := make(chan struct{})
			go func() {
				defer close(sent)
				defer func() { _ = writer.Close() }()
				time.Sleep(140 * time.Millisecond)
				_, _ = io.WriteString(writer, "{\"usage\":{\"prompt_tokens\":7}}")
			}()
			if cancelClient {
				cancel()
			}
			body, err := ReadUpstreamResponseBody(response.Body, nil, c, openAITooLargeError)
			<-sent
			if cancelClient {
				if err != nil || !strings.Contains(string(body), "\"prompt_tokens\":7") {
					t.Fatalf("client cancellation must permit final usage within the drain window: %s %v", body, err)
				}
			} else if !IsTurnBudgetError(err) {
				t.Fatalf("live buffered read must honor its first-output budget: %v", err)
			}
			if rec.Body.Len() != 0 {
				t.Fatal("read budget must not precommit a downstream response")
			}
		})
	}
}

// A minimal, checksummed AWS chunk for end-to-end downstream tests.
func httpBoundaryBedrockFrame(event string) []byte {
	header := []byte("\x0b:event-type\x07\x00\x05chunk")
	payload := []byte(`{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(event)) + `"}`)
	frame := make([]byte, 12+len(header)+len(payload)+4)
	binary.BigEndian.PutUint32(frame, uint32(len(frame)))
	binary.BigEndian.PutUint32(frame[4:], uint32(len(header)))
	binary.BigEndian.PutUint32(frame[8:], crc32.ChecksumIEEE(frame[:8]))
	copy(frame[12:], header)
	copy(frame[12+len(header):], payload)
	binary.BigEndian.PutUint32(frame[len(frame)-4:], crc32.ChecksumIEEE(frame[:len(frame)-4]))
	return frame
}

func TestHTTPBoundaryPreviouslyUnguardedStreamsDrainOnFlushFailure(t *testing.T) {
	for _, path := range []string{"bedrock", "gemini", "responses", "chat"} {
		t.Run(path, func(t *testing.T) {
			rec := &flushFailureRecorder{ResponseRecorder: httptest.NewRecorder(), err: errors.New("downstream flush failed")}
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/", nil)
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			wire := []byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude\",\"usage\":{\"input_tokens\":7}}}\n\n")
			if path == "bedrock" {
				wire = httpBoundaryBedrockFrame(`{"type":"message_start","message":{"usage":{"input_tokens":7}}}`)
			} else if path == "gemini" {
				wire = []byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7}}\n\n")
			}
			go func() { _, _ = writer.Write(wire) }()
			// Keep the upstream open after its first event: only a bounded drain
			// can release this call after the downstream Flush fails.
			watchdog := time.AfterFunc(3*time.Second, func() { _ = reader.Close() })
			defer watchdog.Stop()
			resp := httpStreamContractResponse("")
			resp.Body = reader
			svc := &GatewayService{}
			start := time.Now()
			var disconnected bool
			var input int
			switch path {
			case "bedrock":
				r, _ := svc.handleBedrockStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, start, "claude")
				disconnected, input = r.clientDisconnect, r.usage.InputTokens
			case "gemini":
				g := &AntigravityGatewayService{settingService: &SettingService{}}
				r, _ := g.handleGeminiStreamingResponse(c, resp, start)
				disconnected, input = r.clientDisconnect, r.usage.InputTokens
			case "responses":
				r, _ := svc.handleResponsesStreamingResponse(resp, c, "claude", "claude", nil, start, apicompat.ResponsesClientToolMapping{}, false)
				disconnected, input = r.ClientDisconnect, r.Usage.InputTokens
			case "chat":
				r, _ := svc.handleCCStreamingFromAnthropic(resp, c, "claude", "claude", nil, start, false)
				disconnected, input = r.ClientDisconnect, r.Usage.InputTokens
			}
			if !disconnected || input != 7 || time.Since(start) > 2*time.Second {
				t.Fatalf("unbounded/misattributed write failure: disconnected=%v input=%d elapsed=%s", disconnected, input, time.Since(start))
			}
		})
	}
}

func TestHTTPBoundaryBedrockHeartbeatDoesNotMaskIdleOrCountAsOutput(t *testing.T) {
	c, rec := httpStreamContractContext()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	resp := httpStreamContractResponse("")
	resp.Body = reader
	svc := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		StreamKeepaliveInterval: 1, StreamDataIntervalTimeout: 2,
	}}}
	start := time.Now()
	r, err := svc.handleBedrockStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, start, "claude")
	if err == nil || !strings.Contains(err.Error(), "stream data interval timeout") || time.Since(start) > 3*time.Second {
		t.Fatalf("heartbeat must not extend upstream idle timeout: %v", err)
	}
	if !strings.Contains(rec.Body.String(), "event: ping\ndata: {\"type\":\"ping\"}\n\n") || !rec.Flushed {
		t.Fatal("silent upstream must receive a flushed Anthropic heartbeat")
	}
	if r.firstTokenMs != nil || r.usage.HasObservedTokens() {
		t.Fatal("synthetic heartbeat must not become TTFT or billable usage")
	}
}

func TestHTTPBoundaryAdditionalSSEDrainsOnFlushFailure(t *testing.T) {
	for _, path := range []string{"gemini_messages", "gemini_chat", "gemini_native", "antigravity_upstream", "antigravity_compat", "antigravity_claude", "images", "images_raw", "responses_chat", "responses_messages", "native_anthropic"} {
		t.Run(path, func(t *testing.T) {
			rec := &flushFailureRecorder{ResponseRecorder: httptest.NewRecorder(), err: io.ErrClosedPipe}
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/", nil)
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			wire := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7}}\n\n"
			if path == "antigravity_upstream" || path == "native_anthropic" {
				wire = "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n"
			} else if path == "responses_chat" || path == "responses_messages" {
				wire = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"
			} else if path == "images" || path == "images_raw" {
				wire = "data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"aGVsbG8=\"}\n\n"
			}
			go func() { _, _ = io.WriteString(writer, wire) }()
			watchdog := time.AfterFunc(3*time.Second, func() { _ = reader.Close() })
			defer watchdog.Stop()
			resp := httpStreamContractResponse("")
			resp.Body = reader
			start := time.Now()
			var disconnected bool
			g := &GeminiMessagesCompatService{}
			ag := &AntigravityGatewayService{settingService: &SettingService{}}
			switch path {
			case "gemini_messages":
				r, _ := g.handleStreamingResponse(c, resp, start, "gemini")
				disconnected = r.clientDisconnect
			case "gemini_chat":
				r, _ := g.handleChatCompletionsStreamingResponseFromGemini(c, resp, start, "gemini", false, true)
				disconnected = r.clientDisconnect
			case "gemini_native":
				r, _ := g.handleNativeStreamingResponse(c, resp, start, false, &Account{ID: 1}, "")
				disconnected = r.clientDisconnect
			case "antigravity_upstream":
				r := ag.streamUpstreamResponse(c, resp, start)
				disconnected = r.clientDisconnect
			case "antigravity_compat":
				r, _ := ag.handleAntigravityCompatStream(c, resp, start, "gemini", newAntigravityChatStreamAdapter("gemini", true), "test")
				disconnected = r != nil && r.clientDisconnect
			case "antigravity_claude":
				r, _ := ag.handleClaudeStreamingResponse(c, resp, start, "gemini")
				disconnected = r != nil && r.clientDisconnect
			case "responses_chat":
				r, _ := (&OpenAIGatewayService{}).handleChatStreamingResponse(resp, c, &Account{ID: 1}, "m", "m", "m", start, 0)
				disconnected = r != nil && r.ClientDisconnect
			case "responses_messages":
				r, _ := (&OpenAIGatewayService{}).handleAnthropicStreamingResponse(resp, c, &Account{ID: 1}, "m", "m", "m", start)
				disconnected = r != nil && r.ClientDisconnect
			case "native_anthropic":
				r, _ := (&OpenAIGatewayService{}).handleNativeAnthropicStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1}, "m", "m", "m", nil, start)
				disconnected = r != nil && r.ClientDisconnect
			case "images_raw":
				_, _, _, _, err := (&OpenAIGatewayService{}).handleOpenAIImagesStreamingResponse(resp, c, start, nil)
				disconnected = err != nil
			case "images":
				_, _, _, _, err := (&OpenAIGatewayService{}).handleOpenAIImagesOAuthStreamingResponse(resp, c, start, "b64_json", "image_generation", "image")
				disconnected = errors.Is(err, context.Canceled)
			}
			if !disconnected || time.Since(start) > 2*time.Second {
				t.Fatalf("write failure did not release stream: disconnected=%v elapsed=%s", disconnected, time.Since(start))
			}
			if c.Request.Context().Err() != nil {
				t.Fatal("write guard must not cancel caller-owned context")
			}
		})
	}
}
