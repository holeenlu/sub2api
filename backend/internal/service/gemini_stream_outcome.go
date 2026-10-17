package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
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
	source        io.Reader
	line          []byte
	terminal      bool
	failure       error
	pending       error
	limit         int
}

func newGeminiTerminalReader(r io.Reader, limit int, contexts ...*gin.Context) *geminiTerminalReader {
	if limit <= 0 {
		limit = defaultMaxLineSize
	}
	reader := &geminiTerminalReader{source: r, limit: limit}
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

func (r *geminiTerminalReader) observe(line []byte) {
	// Keep the first protocol failure even if a later frame claims success.
	if r.failure != nil {
		return
	}
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(line[5:])
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
func (r *geminiTerminalReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.pending != nil {
		return 0, r.pending
	}
	n, err := r.source.Read(p)
	rest := p[:n]
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, '\n')
		size := len(rest)
		if i >= 0 {
			size = i + 1
		}
		if len(r.line)+size > r.limit {
			if r.failure == nil {
				r.failure = ErrUpstreamResponseBodyTooLarge
			}
			r.line = nil
		} else {
			r.line = append(r.line, rest[:size]...)
		}
		if i >= 0 {
			r.observe(r.line)
			r.line = r.line[:0]
		}
		rest = rest[size:]
	}
	if err != nil {
		r.observe(r.line)
		r.line = nil
		switch {
		case r.failure != nil:
			r.pending = newOpenAIUpstreamStreamReadError(r.failure)
		case r.terminal:
			r.pending = io.EOF
		case errors.Is(err, io.EOF):
			r.pending = newOpenAIUpstreamStreamReadError(ErrOpenAIUpstreamStreamTruncated)
		default:
			r.pending = err
		}
	} else if r.failure != nil {
		r.pending = newOpenAIUpstreamStreamReadError(r.failure)
	}
	if n > 0 {
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
