package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Gemini has no terminal usage-only frame. Share this transport guard across
// native, translated and aggregated readers so EOF cannot fabricate success.
// Keep the first protocol error and delay read errors until returned bytes have
// been processed (including the last, unterminated SSE line and its usage).
const geminiUsageReportedKey = "gemini_usage_reported"

type geminiTerminalReader struct {
	usageReported *atomic.Bool
	scanner       *bufio.Scanner
	frame         sseEventBuffer
	output        string
	terminal      bool
	failure       error
	pending       error
	limit         int
}

func newGeminiTerminalReader(r io.Reader, limit int, contexts ...*gin.Context) *geminiTerminalReader {
	if limit <= 0 {
		limit = defaultMaxLineSize
	}
	scanner := bufio.NewScanner(r)
	scanner.Split(newSSEScanLines())
	scanner.Buffer(make([]byte, min(4096, limit)), limit)
	reader := &geminiTerminalReader{scanner: scanner, limit: limit}
	if len(contexts) > 0 && contexts[0] != nil {
		reader.usageReported = &atomic.Bool{}
		contexts[0].Set(geminiUsageReportedKey, reader.usageReported)
	}
	return reader
}

func geminiUsageWasReported(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, _ := c.Get(geminiUsageReportedKey)
	reported, ok := value.(*atomic.Bool)
	return ok && reported.Load()
}

func (r *geminiTerminalReader) observe(payload []byte) {
	// Keep the first protocol failure even if a later frame claims success.
	if r.failure != nil {
		return
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return
	}
	if !gjson.ValidBytes(payload) {
		r.fail("invalid Gemini stream event")
		return
	}
	inner, _ := unwrapGeminiResponse(payload)
	if usage := gjson.GetBytes(inner, "usageMetadata"); usage.IsObject() && r.usageReported != nil {
		r.usageReported.Store(true)
	}
	if gjson.GetBytes(inner, "error").Exists() {
		r.fail("Gemini stream reported an upstream error")
		return
	}
	if reason := gjson.GetBytes(inner, "promptFeedback.blockReason").String(); reason != "" && reason != "BLOCK_REASON_UNSPECIFIED" {
		r.terminal = true
	}
	reason := gjson.GetBytes(inner, "candidates.0.finishReason").String()
	if reason != "" && reason != "FINISH_REASON_UNSPECIFIED" {
		r.terminal = true
	}

}
func (r *geminiTerminalReader) fail(message string) {
	if r.failure == nil {
		r.failure = errors.New(message)
	}
}

// Normalize complete SSE events once, before every existing Gemini consumer.
// The current Gemini contract also accepts a valid final JSON frame at EOF
// (TestGeminiStreamsRequireProtocolTerminal). Keep that narrow compatibility
// here until its callers migrate; it never makes a nonterminal EOF successful.
func (r *geminiTerminalReader) emitFrame() {
	frame := parseSSEEvent(r.frame.lines)
	if frame.hasData {
		r.observe([]byte(frame.data))
		if strings.Contains(frame.data, "\n") && json.Valid([]byte(frame.data)) {
			var compact bytes.Buffer
			_ = json.Compact(&compact, []byte(frame.data))
			data := compact.String()
			r.output = frame.block("", &data)
		} else {
			r.output = frame.block("", nil)
		}
	} else {
		r.output = frame.block("", nil)
	}
	r.frame.reset()
}

func (r *geminiTerminalReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.output) == 0 && r.pending == nil {
		if r.scanner.Scan() {
			line := r.scanner.Text()
			if line != "" {
				if err := r.frame.addLine(line, r.limit); err != nil {
					r.pending = newOpenAIUpstreamStreamReadError(ErrUpstreamResponseBodyTooLarge)
				}
				continue
			}
			if len(r.frame.lines) > 0 {
				r.emitFrame()
			} else {
				r.output = "\n"
			}
			if r.failure != nil {
				r.pending = newOpenAIUpstreamStreamReadError(r.failure)
			}
			continue
		}
		// Preserve measured final usage even if the next read returns an error.
		if len(r.frame.lines) > 0 {
			r.emitFrame()
		}
		switch {
		case r.failure != nil:
			r.pending = newOpenAIUpstreamStreamReadError(r.failure)
		case r.terminal:
			r.pending = io.EOF
		case r.scanner.Err() != nil:
			r.pending = r.scanner.Err()
		default:
			r.pending = newOpenAIUpstreamStreamReadError(ErrOpenAIUpstreamStreamTruncated)
		}
	}
	if len(r.output) > 0 {
		n := copy(p, r.output)
		r.output = r.output[n:]
		return n, nil
	}
	return 0, r.pending
}

func reportGeminiStreamFailure(c *gin.Context, err error, protocol string) {
	if err == nil || IsResponseCommitted(c) {
		return
	}
	MarkResponseCommitted(c)
	if errors.Is(err, context.Canceled) || (c.Request != nil && c.Request.Context().Err() != nil) {
		MarkOpsClientCancellation(c, true)
		return
	}
	code, message := classifyOpenAIUpstreamStreamReadError(err)
	var wrapped *openAIUpstreamStreamReadError
	if errors.As(err, &wrapped) {
		code, message = wrapped.clientCode, wrapped.clientMessage
	}
	if _, exists := GetOpsStreamError(c); !exists {
		MarkOpsStreamError(c, code, message, http.StatusBadGateway)
	}
	payload := map[string]any{"error": map[string]any{"type": "upstream_error", "code": code, "message": message}}
	switch protocol {
	case "anthropic":
		payload["type"] = "error"
		writeSSE(c.Writer, "error", payload)
	case "gemini":
		payload = map[string]any{"error": map[string]any{"code": http.StatusBadGateway, "status": "UNAVAILABLE", "message": message}}
		writeSSE(c.Writer, "", payload)
	default:
		writeSSE(c.Writer, "", payload)
	}
	c.Writer.Flush()
}

// Return only measured facts on failure. A model name alone never proves an
// image was generated; do not apply the successful-request image fallback.
func geminiPartialForwardResult(c *gin.Context, resp *http.Response, usage *ClaudeUsage, first *int, start time.Time, model, upstream string, stream bool, imageSize string, effort ...*string) *ForwardResult {
	if usage == nil {
		usage = &ClaudeUsage{}
	}
	images := observedGeminiImageOutputs(c)
	if c != nil && c.Request != nil {
		source := "unknown"
		if geminiUsageWasReported(c) {
			source = "upstream_reported"
		}
		logger.FromContext(c.Request.Context()).Info("gateway.partial_usage", zap.String("usage_source", source), zap.Int("input_tokens", usage.InputTokens), zap.Int("output_tokens", usage.OutputTokens), zap.Int("observed_images", images))
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadInputTokens == 0 && usage.CacheCreationInputTokens == 0 && usage.ImageOutputTokens == 0 && images == 0 {
		return nil
	}
	id := resp.Header.Get("x-request-id")
	if id == "" {
		id = resp.Header.Get("x-goog-request-id")
	}
	var reasoning *string
	if len(effort) > 0 {
		reasoning = effort[0]
	}
	canceled := false
	if outcome, ok := GetOpsStreamError(c); ok {
		canceled = outcome.Code == OpsClientCanceledCode
	}
	return &ForwardResult{ReasoningEffort: reasoning, ClientDisconnect: canceled, RequestID: id, UpstreamHeaders: resp.Header, Usage: *usage, Model: model, UpstreamModel: upstream, Stream: stream, Duration: time.Since(start), FirstTokenMs: first, ImageCount: images, ImageSize: normalizeOpenAIImageSizeTier(imageSize), ImageInputSize: imageSize, UpstreamResponseModel: observedUpstreamResponseModel(c), UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c)}
}

// Capture the first downstream write failure while existing readers finish
// collecting trusted usage. The owner decides whether generation had completed.
type geminiStreamWriter struct {
	io.Writer
	err error
}

func (w *geminiStreamWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.Writer.Write(p)
	if err != nil {
		w.err = err
	}
	return n, err
}
