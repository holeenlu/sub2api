package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const anthropicCompatUsageDrainTimeout = 1200 * time.Millisecond

var errAnthropicCompatStreamIdle = errors.New("stream data interval timeout")

type anthropicCompatStreamResult struct {
	usage              ClaudeUsage
	firstTokenMs       *int
	clientDisconnected bool
}

// Leave early upstream errors uncommitted only for callers that own failover.
func (r anthropicCompatStreamResult) canDeferError(c *gin.Context, err error, writtenBefore int) bool {
	var upstreamErr *sseStreamErrorEventError
	return errors.As(err, &upstreamErr) && !r.clientDisconnected && !r.usage.HasObservedTokens() &&
		(c.Request == nil || c.Request.Context().Err() == nil) && downstreamStreamError(c) == nil &&
		OpenAICompactKeepaliveAdjustedWrittenSize(c) == writtenBefore
}

func (r anthropicCompatStreamResult) forwardResult(resp *http.Response, model, upstreamModel string, effort *string, stream bool, start time.Time) *ForwardResult {
	return &ForwardResult{
		RequestID: resp.Header.Get("x-request-id"), UpstreamHeaders: resp.Header,
		Usage: r.usage, Model: model, UpstreamModel: upstreamModel, ReasoningEffort: effort,
		Stream: stream, Duration: time.Since(start), FirstTokenMs: r.firstTokenMs,
		ClientDisconnect: r.clientDisconnected,
	}
}

// readAnthropicCompatStream converts complete frames from the shared Anthropic
// reader. Tool aliases are restored before either protocol converter sees them.
func (s *GatewayService) readAnthropicCompatStream(
	resp *http.Response, c *gin.Context, start time.Time,
	consume func(*apicompat.AnthropicStreamEvent, string, ClaudeUsage) (bool, error),
) (result anthropicCompatStreamResult, err error) {
	var usage ClaudeUsage
	rw := toolNameRewriteFromContext(c)
	result, err = s.readAnthropicSSEStream(resp, c, start, nil, func(frame sseEvent, disconnected bool) (bool, error) {
		if !frame.hasData || strings.TrimSpace(frame.data) == "" {
			return false, nil
		}
		payload := restoreToolNamesInBytes([]byte(frame.data), rw)
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			// The shared reader returns the original typed error for error frames.
			if strings.TrimSpace(frame.eventName) == "error" {
				return false, nil
			}
			return false, fmt.Errorf("invalid Anthropic SSE event: %w", err)
		}
		if event.Type == "" {
			event.Type = strings.TrimSpace(frame.eventName)
		}
		if event.Message != nil && event.Type == "message_start" {
			mergeAnthropicUsage(&usage, event.Message.Usage)
		}
		if event.Usage != nil {
			mergeAnthropicUsage(&usage, *event.Usage)
		}
		if disconnected || event.Type == "error" || strings.TrimSpace(frame.eventName) == "error" {
			return false, nil
		}
		normalizeAnthropicEventUsageForResponses(&event, usage)
		return consume(&event, frame.data, usage)
	})
	result.usage = usage
	return result, err
}

// readAnthropicSSEStream owns the one bounded frame reader used by compatibility
// conversion and native Messages relay. Consumers still observe frames while
// disconnected for metering, but must not write. Only a complete data-bearing
// message_stop after message_start completes the response; EOF never adds one.
func (s *GatewayService) readAnthropicSSEStream(
	resp *http.Response, c *gin.Context, start time.Time,
	heartbeat func() bool,
	consume func(sseEvent, bool) (bool, error),
) (result anthropicCompatStreamResult, err error) {
	limit := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		limit = s.cfg.Gateway.MaxLineSize
	}
	pump := newSSEReadPump(resp.Body, limit, nil, false)
	defer pump.Close()
	events := pump.events
	var idle *time.Timer
	var idleC <-chan time.Time
	var interval time.Duration
	if s.cfg != nil && s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
		interval = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		idle = time.NewTimer(interval)
		idleC = idle.C
		defer idle.Stop()
	}

	var canceled, writeCanceled <-chan struct{}
	if writeCtx := streamWriteContext(c); writeCtx != nil {
		writeCanceled = writeCtx.Done()
	}
	if c != nil && c.Request != nil {
		canceled = c.Request.Context().Done()
	}
	var drain *time.Timer
	var drainC <-chan time.Time
	var drainDeadline time.Time
	defer func() {
		if drain != nil {
			drain.Stop()
		}
	}()
	disconnect := func() {
		if !result.clientDisconnected {
			result.clientDisconnected = true
			canceled, writeCanceled = nil, nil
			drainDeadline = time.Now().Add(anthropicCompatUsageDrainTimeout)
			drain = time.NewTimer(anthropicCompatUsageDrainTimeout)
			drainC = drain.C
		}
	}
	var keepalive *time.Timer
	var keepaliveC <-chan time.Time
	var keepaliveInterval time.Duration
	if heartbeat != nil && s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		keepaliveInterval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
		keepalive = time.NewTimer(keepaliveInterval)
		keepaliveC = keepalive.C
		defer keepalive.Stop()
	}
	pending := sseEventBuffer{}
	var lifecycle anthropicStreamLifecycle
	for {
		// Observe cancellation before processing already queued upstream data.
		select {
		case <-canceled:
			disconnect()
		case <-writeCanceled:
			disconnect()
		default:
		}
		if !drainDeadline.IsZero() && !time.Now().Before(drainDeadline) {
			return result, fmt.Errorf("Anthropic usage drain expired before message_stop: %w", context.DeadlineExceeded)
		}
		select {
		case <-canceled:
			disconnect()
		case <-writeCanceled:
			disconnect()
		case <-drainC:
			return result, fmt.Errorf("Anthropic usage drain expired before message_stop: %w", context.DeadlineExceeded)
		case <-idleC:
			if remaining := time.Until(time.Unix(0, pump.lastRead.Load()).Add(interval)); remaining > 0 {
				idle.Reset(remaining)
				continue
			}
			return result, errAnthropicCompatStreamIdle
		case <-keepaliveC:
			// Pending upstream lines have never been written downstream. A
			// heartbeat is still at a downstream frame boundary while they wait.
			if !result.clientDisconnected && heartbeat() {
				disconnect()
			}
			keepalive.Reset(keepaliveInterval)
		case scanned, ok := <-events:
			if !ok {
				return result, fmt.Errorf("Anthropic stream ended without message_stop: %w", io.ErrUnexpectedEOF)
			}
			if scanned.err != nil {
				return result, fmt.Errorf("read Anthropic stream: %w", scanned.err)
			}
			if idle != nil {
				idle.Reset(interval)
			}
			if scanned.line != "" {
				if err := pending.addLine(scanned.line, limit); err != nil {
					return result, err
				}
				continue
			}
			if len(pending.lines) == 0 {
				continue
			}
			frame := parseSSEEvent(pending.lines)
			eventType := strings.TrimSpace(frame.eventName)
			hasData := frame.hasData && strings.TrimSpace(frame.data) != ""
			isError := eventType == "error"
			if hasData {
				if !gjson.Valid(frame.data) || !gjson.Parse(frame.data).IsObject() {
					if !isError {
						return result, fmt.Errorf("invalid Anthropic SSE event JSON")
					}
				} else if dataType := gjson.Get(frame.data, "type").String(); dataType != "" {
					eventType = dataType
				}
			}
			isError = isError || (hasData && eventType == "error")
			terminal, lifecycleErr := lifecycle.observe(frame.eventName, frame.data)
			if lifecycleErr != nil {
				return result, lifecycleErr
			}
			if !result.clientDisconnected && hasData && !isError && eventType != "ping" && result.firstTokenMs == nil {
				ms := int(time.Since(start).Milliseconds())
				result.firstTokenMs = &ms
			}
			closed, err := consume(frame, result.clientDisconnected)
			pending.reset() // frame.lines remains valid until the consumer has returned.
			if err != nil {
				return result, err
			}
			if closed {
				disconnect()
			}
			if keepalive != nil {
				keepalive.Reset(keepaliveInterval)
			}
			if isError {
				return result, &sseStreamErrorEventError{RawData: frame.data}
			}
			if terminal {
				return result, nil
			}
		}
	}
}

// Both buffered compatibility responses assemble the same Anthropic message.
func (s *GatewayService) collectAnthropicCompatResponse(resp *http.Response, c *gin.Context, start time.Time) (*apicompat.AnthropicResponse, anthropicCompatStreamResult, error) {
	var message *apicompat.AnthropicResponse
	result, err := s.readAnthropicCompatStream(resp, c, start, func(event *apicompat.AnthropicStreamEvent, _ string, _ ClaudeUsage) (bool, error) {
		if event.Type == "message_start" {
			message = event.Message
		}
		if message == nil {
			return false, nil
		}
		switch event.Type {
		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != "" {
				message.StopReason = apicompat.AnthropicStopReasonPtr(event.Delta.StopReason)
			}
		case "content_block_start":
			if event.ContentBlock != nil {
				message.Content = append(message.Content, *event.ContentBlock)
			}
		case "content_block_delta":
			if event.Index == nil || *event.Index < 0 || *event.Index >= len(message.Content) || event.Delta == nil {
				break
			}
			block := &message.Content[*event.Index]
			switch event.Delta.Type {
			case "text_delta":
				block.Text += event.Delta.Text
			case "thinking_delta":
				block.Thinking += event.Delta.Thinking
			case "signature_delta":
				block.Signature += event.Delta.Signature
			case "input_json_delta":
				block.Input = appendRawJSON(block.Input, event.Delta.PartialJSON)
			}
		}
		return false, nil
	})
	if message != nil {
		message.Usage = apicompat.AnthropicUsage{
			InputTokens: result.usage.InputTokens, OutputTokens: result.usage.OutputTokens,
			CacheReadInputTokens: result.usage.CacheReadInputTokens, CacheCreationInputTokens: result.usage.CacheCreationInputTokens,
		}
	}
	return message, result, err
}

func anthropicCompatError(err error) (code, message string) {
	code, message = "upstream_error", "Upstream stream failed before message_stop"
	var streamErr *sseStreamErrorEventError
	if errors.As(err, &streamErr) {
		if kind := gjson.Get(streamErr.RawData, "error.type").String(); kind != "" {
			code = kind
		}
		if detail := sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage([]byte(streamErr.RawData))); detail != "" {
			message = detail
		}
	} else if errors.Is(err, io.ErrUnexpectedEOF) {
		code, message = "upstream_missing_terminal", "Upstream stream ended without message_stop"
	} else if errors.Is(err, errAnthropicCompatStreamIdle) {
		code, message = "upstream_timeout", "Upstream stream data interval timeout"
	}
	return code, message
}
