package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
)

const (
	maxOpenAIConcatenatedJSONDocuments = 16
	maxOpenAIConcatenatedJSONBytes     = 16 * 1024 * 1024
)

// splitOpenAIConcatenatedJSONDocuments recognizes the narrow corruption shape
// produced when multiple complete Responses events arrive in one transport
// message. Other malformed payloads are left untouched for normal error paths.
func splitOpenAIConcatenatedJSONDocuments(payload []byte) ([][]byte, bool) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || len(payload) > maxOpenAIConcatenatedJSONBytes || json.Valid(payload) {
		return nil, false
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	documents := make([][]byte, 0, 2)
	for {
		var raw json.RawMessage
		err := decoder.Decode(&raw)
		if err != nil {
			if err == io.EOF && len(documents) > 1 {
				return documents, true
			}
			return nil, false
		}
		raw = bytes.TrimSpace(raw)
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, false
		}
		eventType := strings.TrimSpace(envelope.Type)
		if eventType == "" || strings.ContainsAny(eventType, "\r\n") {
			return nil, false
		}
		if len(documents) == maxOpenAIConcatenatedJSONDocuments {
			return nil, false
		}
		documents = append(documents, raw)
	}
}

// openAISSEFrame carries one validated, bounded event and its protocol facts.
// Original lines remain intact unless a consumer actually changes its data.
// Newline spelling is normalized by the shared SSE line scanner.
type openAISSEFrame struct {
	sseEvent
	eventType string
	rewritten bool
}

func newOpenAISSEFrame(event sseEvent) openAISSEFrame {
	return openAISSEFrame{sseEvent: event, eventType: effectiveOpenAISSEEventType([]byte(event.data), event.eventName)}
}

func (f openAISSEFrame) block(data string) string {
	if f.rewritten || data != f.data {
		return f.sseEvent.block("", &data)
	}
	return f.sseEvent.block("", nil)
}

func (f openAISSEFrame) compat() openAICompatSSEFrame {
	return openAICompatSSEFrame{EventType: f.eventType, Data: f.data}
}

type openAISSEJSONDocumentScanner struct {
	scanner      *bufio.Scanner
	pending      []openAISSEFrame
	current      openAISSEFrame
	frame        sseEventBuffer
	limit        int
	err          error
	beforeOutput *atomic.Bool
}

func newOpenAISSEJSONDocumentScanner(scanner *bufio.Scanner, limits ...int) *openAISSEJSONDocumentScanner {
	limit := defaultMaxLineSize
	if len(limits) > 0 && limits[0] > 0 {
		limit = limits[0]
	}
	return &openAISSEJSONDocumentScanner{scanner: scanner, limit: limit}
}

func (s *openAISSEJSONDocumentScanner) Scan() bool {
	if s.err != nil {
		return false
	}
	if len(s.pending) > 0 {
		s.current = s.pending[0]
		s.pending[0] = openAISSEFrame{}
		s.pending = s.pending[1:]
		return true
	}
	for s.scanner != nil && s.scanner.Scan() {
		line := s.scanner.Text()
		if line != "" {
			limit := s.limit
			guarded := s.beforeOutput != nil && s.beforeOutput.Load()
			if guarded {
				limit = min(limit, openAIFirstOutputStageMaxBytes+openAIFirstOutputScannerFramingAllowance)
			}
			if err := s.frame.addLine(line, limit); err != nil {
				s.err = err
				if guarded {
					s.err = errOpenAIFirstOutputScannerLimit
				}
				return false
			}
			continue
		}
		if len(s.frame.lines) == 0 {
			continue
		}
		frame := newOpenAISSEFrame(parseSSEEvent(s.frame.lines))
		// Ownership moves to the consumer; subsequent scans must not clear or
		// reuse the backing lines while this frame is queued or being written.
		s.frame = sseEventBuffer{}
		data := strings.TrimSpace(frame.data)
		if frame.hasData && data != "" && data != "[DONE]" && !json.Valid([]byte(data)) {
			// Repair only the existing narrow corruption shape. Never forward
			// opaque invalid data that would hide terminal or billing facts.
			documents, repaired := splitOpenAIConcatenatedJSONDocuments([]byte(data))
			if !repaired {
				s.err = errors.New("invalid upstream SSE event JSON")
				return false
			}
			for i, document := range documents {
				value := string(document)
				if i == 0 {
					frame.data, frame.rewritten = value, true
					frame.eventType = effectiveOpenAISSEEventType(document, frame.eventName)
					s.pending = append(s.pending, frame)
				} else {
					eventType := effectiveOpenAISSEEventType(document, "")
					s.pending = append(s.pending, newOpenAISSEFrame(sseEvent{
						lines:     []string{"event: " + eventType, "data: " + value},
						eventName: eventType, data: value, hasEvent: true, hasData: true,
					}))
				}
			}
			return s.Scan()
		}
		s.current = frame
		return true
	}
	if s.scanner != nil {
		s.err = s.scanner.Err()
	}
	if s.err == nil && len(s.frame.lines) > 0 {
		s.err = io.ErrUnexpectedEOF
	}
	return false
}

func (s *openAISSEJSONDocumentScanner) Frame() openAISSEFrame { return s.current }
func (s *openAISSEJSONDocumentScanner) Err() error            { return s.err }
