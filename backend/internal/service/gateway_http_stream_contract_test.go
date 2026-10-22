//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
