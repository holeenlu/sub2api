package service

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompatibilitySSEReadersUseByteActivityAndBoundIdle(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, tc := range []struct {
		name string
		cc   bool
		read func(*OpenAIGatewayService, *http.Response, *gin.Context) error
	}{
		{"buffered_responses", false, func(s *OpenAIGatewayService, r *http.Response, c *gin.Context) error {
			_, e := s.readOpenAIHTTPResponseBody(r, c)
			return e
		}},
		{"buffered_compat", false, func(s *OpenAIGatewayService, r *http.Response, c *gin.Context) error {
			_, _, _, e := s.readOpenAICompatBufferedTerminal(r, c, nil, "test", "")
			return e
		}},
		{"responses_to_chat", false, func(s *OpenAIGatewayService, r *http.Response, c *gin.Context) error {
			_, e := s.handleChatStreamingResponse(r, c, account, "gpt-5", "gpt-5", "gpt-5", time.Now(), 0)
			return e
		}},
		{"responses_to_messages", false, func(s *OpenAIGatewayService, r *http.Response, c *gin.Context) error {
			_, e := s.handleAnthropicStreamingResponse(r, c, account, "gpt-5", "gpt-5", "gpt-5", time.Now())
			return e
		}},
		{"chat_fallback", true, func(s *OpenAIGatewayService, r *http.Response, c *gin.Context) error {
			return s.scanCCStream(c, r, "test", "", time.Now(), func(*apicompat.ChatCompletionsChunk) {}).Err
		}},
		{"raw_chat", true, func(s *OpenAIGatewayService, r *http.Response, c *gin.Context) error {
			_, e := s.streamRawChatCompletions(c, r, account, "gpt-5", "gpt-5", "gpt-5", nil, nil, time.Now(), 0)
			return e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1}}}
			for _, silent := range []bool{false, true} {
				reader, writer := io.Pipe()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				resp := &http.Response{Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
				wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":7,\"output_tokens\":1}}}\n\n"
				if tc.cc {
					wire = "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"
				}
				done := make(chan struct{})
				go func() {
					defer close(done)
					if silent {
						_, _ = io.WriteString(writer, ": heartbeat\n\n")
						return
					}
					defer func() { _ = writer.Close() }()
					for len(wire) > 0 {
						n := min(len(wire), 12)
						if _, e := io.WriteString(writer, wire[:n]); e != nil {
							return
						}
						wire = wire[n:]
						time.Sleep(70 * time.Millisecond)
					}
				}()
				err := tc.read(s, resp, c)
				_ = writer.Close()
				<-done
				if silent {
					require.Error(t, err, "a heartbeat followed by silence must time out")
				} else {
					require.NoError(t, err, "fragment bytes refresh upstream activity before any line completes")
				}
			}
		})
	}
}

func TestOpenAISSEReadPumpCompleteFrameContract(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		invalid           bool
	}{
		{"multiline", "id: x\nretry: 4\n: note\ndata: {\ndata: \"type\":\"response.completed\",\ndata: \"response\":{\"usage\":{\"input_tokens\":5}}}\n\n", `"input_tokens":5`, false},
		{"CR", "\ufeffdata: {\"type\":\"response.completed\"}\r\r", "response.completed", false},
		{"split_CRLF", "data: {\"type\":\"response.completed\"}\r\n\r\n", "response.completed", false},
		{"missing_boundary", "data: {\"type\":\"response.completed\"}\n", "", true},
		{"whitespace_not_boundary", "data: {\"type\":\"response.completed\"}\n \n", "", true},
		{"trailing_garbage", "data: {\"type\":\"response.completed\"}garbage\n\n", "", true},
		{"invalid_multiline", "data: {\"type\":\"response.completed\"}\ndata: invalid\n\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := io.NopCloser(iotest.OneByteReader(strings.NewReader(tc.input)))
			pump := newOpenAISSEReadPump(body, 4096)
			defer pump.Close()
			var output []string
			for pump.Next(context.Background(), time.Second, nil, nil) {
				frame := pump.Frame()
				output = append(output, frame.block(frame.data))
			}
			if tc.invalid {
				require.Error(t, pump.Err())
				require.Empty(t, output)
				return
			}
			require.NoError(t, pump.Err())
			require.Contains(t, strings.Join(output, "\n"), tc.want)
			if tc.name == "multiline" {
				for _, field := range []string{"id: x", "retry: 4", ": note"} {
					require.Contains(t, strings.Join(output, ""), field)
				}
			}
		})
	}
}

func TestOpenAISSEReadPumpSilentHeartbeatAndJoin(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	pump := newOpenAISSEReadPump(reader, 1024)
	defer pump.Close()
	beats := time.NewTicker(5 * time.Millisecond)
	defer beats.Stop()
	count := 0
	start := time.Now()
	require.False(t, pump.Next(context.Background(), 50*time.Millisecond, beats.C, func() { count++ }))
	require.ErrorIs(t, pump.Err(), errOpenAISSEIdle)
	require.Positive(t, count)
	require.Less(t, time.Since(start), time.Second)
	pump.Close()
	pump.Close()
	select {
	case <-pump.done:
	default:
		t.Fatal("reader not joined")
	}
}

func TestOpenAISSEReadPumpFragmentedDocumentIsActivity(t *testing.T) {
	reader, writer := io.Pipe()
	pump := newOpenAISSEReadPump(reader, 4096)
	defer pump.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = writer.Close() }()
		for _, piece := range []string{`data: {"type":`, `"response.output_text.delta",`, `"delta":`, `"fragmented"}`, "\n\n"} {
			if _, err := io.WriteString(writer, piece); err != nil {
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
	}()
	defer func() { pump.Close(); <-done }()
	require.True(t, pump.Next(context.Background(), 100*time.Millisecond, nil, nil))
	require.Contains(t, pump.Frame().data, "fragmented")
}

func TestOpenAISSEReadPumpCancelAndBoundedQueue(t *testing.T) {
	for _, input := range []string{"", strings.Repeat("data: {}\n\n", 10000)} {
		t.Run("cancel", func(t *testing.T) {
			reader, writer := io.Pipe()
			pump := newOpenAISSEReadPump(reader, 1024)
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { _ = writer.Close() }()
				if input != "" {
					_, _ = io.WriteString(writer, input)
				} else {
					<-pump.stop
				}
			}()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			for pump.Next(ctx, 0, nil, nil) {
			}
			require.True(t, errors.Is(pump.Err(), context.Canceled))
			pump.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("blocked scanner or producer leaked")
			}
		})
	}
}

func TestOpenAISSEReadPumpFirstOutputDeadlineNotExtendedByComments(t *testing.T) {
	reader, writer := io.Pipe()
	pump := newOpenAISSEReadPump(reader, 1024)
	pump.firstOutputDeadline = time.Now().Add(90 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = writer.Close() }()
		for {
			if _, err := io.WriteString(writer, ": upstream heartbeat\n\n"); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	defer func() { pump.Close(); <-done }()
	start := time.Now()
	for pump.Next(context.Background(), time.Second, nil, nil) {
	}
	require.ErrorIs(t, pump.Err(), errOpenAISSEFirstOutput)
	require.Less(t, time.Since(start), time.Second)
}

func TestOpenAISSEReadPumpIncompleteUsageIsNotAnUpstreamTerminal(t *testing.T) {
	result := &OpenAIForwardResult{Usage: OpenAIUsage{OutputTokens: 7}, UpstreamEndpoint: "/basispoints/api/responses"}
	require.False(t, result.SucceededForScheduling())
	require.Empty(t, result.UpstreamTerminalEvent, "local timeout must not invent an upstream terminal")
	require.Equal(t, 7, result.Usage.OutputTokens)
}

func TestOpenAISSEFrameHandoffPreservesMetadataAndMultilineData(t *testing.T) {
	wire := ": comment\r\nid: request-1\r\nretry: later\r\ndata: {\r\ndata: \"type\":\"response.output_text.delta\",\r\ndata: \"delta\":\"hello\"}\r\nextension: harmless\r\nevent: ignored\r\nevent: response.output_text.delta\r\n\r\n"
	pump := newOpenAISSEReadPump(io.NopCloser(iotest.OneByteReader(strings.NewReader(wire))), 4096)
	defer pump.Close()
	require.True(t, pump.Next(context.Background(), 0, nil, nil))
	frame := pump.Frame()
	require.Equal(t, "response.output_text.delta", frame.eventType)
	require.Equal(t, strings.ReplaceAll(wire, "\r\n", "\n"), frame.block(frame.data))
	require.JSONEq(t, `{"type":"response.output_text.delta","delta":"hello"}`, frame.data)
	// Rewritten JSON can still contain pretty-print newlines. Each line must
	// retain its data prefix or a conforming client would lose part of it.
	changed := strings.Replace(frame.data, "hello", "goodbye", 1)
	out := frame.block(changed)
	require.Contains(t, out, "retry: later\n")
	require.Contains(t, out, "extension: harmless\n")
	reader := bufio.NewScanner(strings.NewReader(out))
	frames := newOpenAISSEJSONDocumentScanner(reader)
	require.True(t, frames.Scan())
	require.JSONEq(t, `{"type":"response.output_text.delta","delta":"goodbye"}`, frames.Frame().data)
	require.False(t, pump.Next(context.Background(), 0, nil, nil), "one complete upstream frame is one handoff")
	require.NoError(t, pump.Err())
}

func TestOpenAISSEFrameHandoffDoesNotReuseQueuedLines(t *testing.T) {
	wire := "id: one\ndata: {\"type\":\"response.created\"}\n\nid: two\ndata: {\"type\":\"response.completed\"}\n\n"
	pump := newOpenAISSEReadPump(io.NopCloser(strings.NewReader(wire)), 4096)
	defer pump.Close()
	require.True(t, pump.Next(context.Background(), 0, nil, nil))
	first := pump.Frame()
	require.True(t, pump.Next(context.Background(), 0, nil, nil))
	require.Contains(t, first.block(first.data), "id: one\n")
	require.NotContains(t, first.block(first.data), "id: two\n")
	require.Equal(t, "response.created", first.eventType)
	require.Equal(t, "response.completed", pump.Frame().eventType)
}

func TestOpenAISSEFrameHandoffGuardAcknowledgesWholeFrame(t *testing.T) {
	var guard atomic.Bool
	guard.Store(true)
	wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"start\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"padding\":\"" + strings.Repeat("x", openAIFirstOutputStageMaxBytes+2048) + "\"}\n\n"
	pump := newSSEReadPump(io.NopCloser(strings.NewReader(wire)), len(wire)+1, &guard, true)
	defer pump.Close()
	require.True(t, pump.Next(context.Background(), 0, nil, nil))
	require.Equal(t, "response.output_text.delta", pump.Frame().eventType)
	guard.Store(false)
	require.True(t, pump.Next(context.Background(), 0, nil, nil), "consumer clears first-output guard before producer scans next frame")
	require.Equal(t, "response.completed", pump.Frame().eventType)
	require.False(t, pump.Next(context.Background(), 0, nil, nil))
	require.NoError(t, pump.Err())
}

func TestOpenAISSEFrameScannerExactBoundAndLineFlood(t *testing.T) {
	for _, tc := range []struct {
		name      string
		wire      string
		limit     int
		wantError bool
	}{
		{"exact", "data: {}\n\n", len("data: {}\n\n"), false},
		{"over", "data: {}\n\n", len("data: {}\n\n") - 1, true},
		{"line_flood", strings.Repeat(":\n", 16*1024+1) + "\n", 128 * 1024, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(tc.wire))
			frames := newOpenAISSEJSONDocumentScanner(scanner, tc.limit)
			if tc.wantError {
				require.False(t, frames.Scan())
				require.ErrorIs(t, frames.Err(), bufio.ErrTooLong)
			} else {
				require.True(t, frames.Scan())
				require.Equal(t, tc.wire, frames.Frame().block(frames.Frame().data))
			}
		})
	}
}

// Exercise the actual reader goroutine, bounded handoff and downstream flush;
// neither this fixture nor its numbers include a real network or generation.
func BenchmarkOpenAISSEReadPumpToFlush(b *testing.B) {
	for _, guarded := range []bool{false, true} {
		name := "steady"
		if guarded {
			name = "first_output"
		}
		b.Run(name, func(b *testing.B) {
			wire := strings.Repeat("id: x\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n", 128)
			b.SetBytes(int64(len(wire)))
			b.ReportAllocs()
			for b.Loop() {
				var guard atomic.Bool
				guard.Store(guarded)
				pump := newSSEReadPump(io.NopCloser(strings.NewReader(wire)), 4096, &guard, true)
				writer := bufio.NewWriterSize(io.Discard, 4096)
				for pump.Next(context.Background(), 0, nil, nil) {
					frame := pump.Frame()
					_, _ = writer.WriteString(frame.block(frame.data))
					_ = writer.Flush()
				}
				pump.Close()
				if err := pump.Err(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
