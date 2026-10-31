package basispoints

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	ssesplit "github.com/Wei-Shaw/sub2api/internal/gateway/protocol/sse"
)

// MaxSSEEventBytes bounds a complete BPS event, including tool batches.
const MaxSSEEventBytes = 32 << 20

type protocolError struct{ error }

func (e protocolError) Unwrap() error { return e.error }

// ErrStreamIncomplete distinguishes clean body EOF without a response terminal
// from a transport read error, retaining the existing EOF contract.
var ErrStreamIncomplete = fmt.Errorf("basispoints stream ended without a terminal: %w", io.ErrUnexpectedEOF)

type streamBody struct {
	*io.PipeReader
	upstream io.ReadCloser
	once     sync.Once
	err      error
	cancel   context.CancelFunc
	done     chan struct{}
	usage    object           // published by done, including withheld correction responses
	failure  *UpstreamFailure // original error fields, also published by done
	cause    error
}

func (b *streamBody) closeUpstream() error {
	b.once.Do(func() { b.err = b.upstream.Close() })
	return b.err
}

func (b *streamBody) Close() error {
	b.cancel()
	readerErr := b.PipeReader.Close()
	upstreamErr := b.closeUpstream()
	<-b.done
	return errors.Join(readerErr, upstreamErr)
}

// ObservedStreamUsage returns reported usage after the converter has stopped.
// Call Close first on an interrupted stream. The snapshot includes withheld
// tool responses; downstream cancellation cannot erase their actual cost.
func ObservedStreamUsage(stream io.ReadCloser) map[string]any {
	body, ok := stream.(interface{ ObservedUsage() map[string]any })
	if !ok {
		return nil
	}
	return body.ObservedUsage()
}

// ObservedStreamFailure retains the original failure independently of the
// normalized client event. As with usage, close the stream before reading it.
func ObservedStreamFailure(stream io.ReadCloser) *UpstreamFailure {
	body, ok := stream.(interface{ ObservedFailure() *UpstreamFailure })
	if !ok {
		return nil
	}
	return body.ObservedFailure()
}

// ObservedStreamCause preserves a repair dispatch/read cause across protocol
// formatting. Close an interrupted stream first, as with usage and failure.
func ObservedStreamCause(stream io.ReadCloser) error {
	body, ok := stream.(interface{ ObservedCause() error })
	if !ok {
		return nil
	}
	return body.ObservedCause()
}

// Stream projects validated events to external SSE consumers. Gateway internals
// consume NewStreamConversion directly. It cannot send corrections; Close
// interrupts input/output and joins the worker.
func (b *Bridge) Stream(upstream io.ReadCloser) io.ReadCloser {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	body := &streamBody{PipeReader: reader, upstream: upstream, cancel: cancel, done: make(chan struct{})}
	conversion := b.NewStreamConversion(func(kind string, raw []byte) error {
		_, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", kind, raw)
		return err
	}, RepairPolicy{})
	go func() {
		defer close(body.done)
		defer cancel()
		stop := context.AfterFunc(ctx, func() { _ = writer.CloseWithError(ctx.Err()); _ = body.closeUpstream() })
		defer stop()
		_, err := conversion.Convert(upstream)
		if err == nil {
			err = conversion.Deliver(b.CommitReplays(conversion.Replays()))
		}
		body.usage, body.failure = conversion.Usage(), conversion.Failure()
		body.cause = conversion.Cause()
		_ = body.closeUpstream()
		_ = writer.CloseWithError(err)
	}()
	return body
}

// Empty lifecycle events and opaque reasoning do not commit visible content.
// Also recognize content carried by done/item events when deltas are absent.
func hasVisibleStreamContent(kind string, payload object) bool {
	partHasText := func(part object) bool {
		return text(part["text"]) != "" || text(part["refusal"]) != ""
	}
	switch kind {
	case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.refusal.delta":
		return text(payload["delta"]) != ""
	case "response.output_text.done", "response.reasoning_summary_text.done", "response.refusal.done":
		return partHasText(payload)
	case "response.content_part.added", "response.content_part.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		part, _ := payload["part"].(object)
		return partHasText(part)
	case "response.output_item.added", "response.output_item.done":
		item, _ := payload["item"].(object)
		field := "content"
		switch text(item["type"]) {
		case "message":
		case "reasoning":
			field = "summary"
		default:
			return false
		}
		parts, _ := item[field].([]any)
		for _, raw := range parts {
			part, _ := raw.(object)
			if partHasText(part) {
				return true
			}
		}
	}
	return false
}

// SSE can carry the event type in its event line rather than its JSON body.
// Normalize only the observer's copy; forwarding still uses the original parser.
func observeUpstreamEvent(observer func([]byte), event string, data []byte, payload object) {
	if observer == nil {
		return
	}
	if text(payload["type"]) == "" && event != "" {
		observed := make(object, len(payload)+1)
		for key, value := range payload {
			observed[key] = value
		}
		observed["type"] = event
		if encoded, err := json.Marshal(observed); err == nil {
			observer(encoded)
		}
		return
	}
	observer(data)
}

func readEvents(reader io.Reader, consume func(string, []byte) error) error {
	scanner := bufio.NewScanner(reader)
	// SSE allows CR, LF and CRLF line endings; bufio.ScanLines only knows LF.
	scanner.Split(ssesplit.ScanLines())
	scanner.Buffer(make([]byte, 64*1024), MaxSSEEventBytes)
	var data strings.Builder
	event := ""
	flush := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		err := consume(event, []byte(strings.TrimSuffix(data.String(), "\n")))
		data.Reset()
		event = ""
		return err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			_, _ = data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			_ = data.WriteByte('\n')
			if data.Len() > MaxSSEEventBytes {
				return protocolError{fmt.Errorf("basispoints SSE event exceeds %d MiB", MaxSSEEventBytes>>20)}
			}
		}
	}
	readErr := scanner.Err()
	if errors.Is(readErr, bufio.ErrTooLong) {
		return protocolError{fmt.Errorf("basispoints SSE line exceeds 16 MiB")}
	}
	// Readers can return final bytes and an error together. As at clean EOF,
	// consume complete JSON before interpreting the late error. A terminal wins;
	// partial JSON and nonterminal tails cannot make a broken connection succeed.
	if readErr != nil && !json.Valid([]byte(data.String())) {
		return readErr
	}
	if err := flush(); err != nil {
		return err
	}
	return readErr
}

func (b *streamBody) ObservedUsage() map[string]any     { <-b.done; return b.usage }
func (b *streamBody) ObservedFailure() *UpstreamFailure { <-b.done; return b.failure }
func (b *streamBody) ObservedCause() error              { <-b.done; return b.cause }
