package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const anthropicCompatUsageDrainTimeout = 1200 * time.Millisecond

type anthropicCompatStreamResult struct {
	usage              ClaudeUsage
	firstTokenMs       *int
	clientDisconnected bool
}

func (r anthropicCompatStreamResult) forwardResult(resp *http.Response, model, upstreamModel string, effort *string, stream bool, start time.Time) *ForwardResult {
	return &ForwardResult{
		RequestID: resp.Header.Get("x-request-id"), UpstreamHeaders: resp.Header,
		Usage: r.usage, Model: model, UpstreamModel: upstreamModel, ReasoningEffort: effort,
		Stream: stream, Duration: time.Since(start), FirstTokenMs: r.firstTokenMs,
		ClientDisconnect: r.clientDisconnected,
	}
}

// readAnthropicCompatStream is shared by both compatibility protocols and their
// JSON/SSE responses. Only complete frames reach conversion; only message_stop
// completes a response. The reader owns the upstream body until termination,
// including a bounded usage drain after downstream cancellation/write failure.
func (s *GatewayService) readAnthropicCompatStream(
	resp *http.Response, c *gin.Context, start time.Time,
	consume func(*apicompat.AnthropicStreamEvent, string, ClaudeUsage) (bool, error),
) (result anthropicCompatStreamResult, err error) {
	limit := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		limit = s.cfg.Gateway.MaxLineSize
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, min(64*1024, limit)), limit)
	type scanEvent struct {
		line string
		err  error
	}
	events := make(chan scanEvent, 1)
	done, readerDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(events)
		send := func(event scanEvent) bool {
			select {
			case events <- event:
				return true
			case <-done:
				return false
			}
		}
		for scanner.Scan() {
			if !send(scanEvent{line: scanner.Text()}) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			send(scanEvent{err: err})
		}
	}()
	defer func() {
		close(done)
		_ = resp.Body.Close() // Unblock a pending HTTP body read, including on message_stop.
		<-readerDone
	}()

	var canceled <-chan struct{}
	if c.Request != nil {
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
			canceled = nil
			drainDeadline = time.Now().Add(anthropicCompatUsageDrainTimeout)
			drain = time.NewTimer(anthropicCompatUsageDrainTimeout)
			drainC = drain.C
		}
	}
	rw := toolNameRewriteFromContext(c)
	pending := anthropicSSEEventBuffer{}
	started := false
	for {
		// Observe cancellation before processing already queued upstream data.
		select {
		case <-canceled:
			disconnect()
		default:
		}
		if !drainDeadline.IsZero() && !time.Now().Before(drainDeadline) {
			return result, fmt.Errorf("Anthropic usage drain expired before message_stop: %w", context.DeadlineExceeded)
		}
		select {
		case <-canceled:
			disconnect()
		case <-drainC:
			return result, fmt.Errorf("Anthropic usage drain expired before message_stop: %w", context.DeadlineExceeded)
		case scanned, ok := <-events:
			if !ok {
				return result, fmt.Errorf("Anthropic stream ended without message_stop: %w", io.ErrUnexpectedEOF)
			}
			if scanned.err != nil {
				return result, fmt.Errorf("read Anthropic stream: %w", scanned.err)
			}
			if scanned.line != "" {
				if err := pending.addLine(scanned.line, limit); err != nil {
					return result, err
				}
				continue
			}
			frame := parseAnthropicSSEEvent(pending.lines)
			pending.reset()
			if !frame.hasData {
				if frame.eventName == "error" {
					return result, &sseStreamErrorEventError{RawData: frame.data}
				}
				continue
			}
			// Restore only Anthropic tool name fields before either protocol converter.
			payload := restoreToolNamesInBytes([]byte(frame.data), rw)
			var event apicompat.AnthropicStreamEvent
			if err := json.Unmarshal(payload, &event); err != nil {
				return result, fmt.Errorf("invalid Anthropic SSE event: %w", err)
			}
			if event.Type == "" {
				event.Type = frame.eventName
			}
			if event.Message != nil && event.Type == "message_start" {
				started = true
				mergeAnthropicUsage(&result.usage, event.Message.Usage)
			}
			if event.Usage != nil {
				mergeAnthropicUsage(&result.usage, *event.Usage)
			}
			if frame.eventName == "error" || event.Type == "error" {
				return result, &sseStreamErrorEventError{RawData: frame.data}
			}
			if event.Type == "message_stop" && !started {
				return result, fmt.Errorf("Anthropic message_stop without message_start")
			}
			normalizeAnthropicEventUsageForResponses(&event, result.usage)
			if !result.clientDisconnected {
				if result.firstTokenMs == nil && event.Type != "ping" {
					ms := int(time.Since(start).Milliseconds())
					result.firstTokenMs = &ms
				}
				closed, err := consume(&event, frame.data, result.usage)
				if err != nil {
					return result, err
				}
				if closed {
					disconnect()
				}
			}
			if event.Type == "message_stop" {
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
	}
	return code, message
}
