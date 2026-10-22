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

type openAISSEJSONDocumentScanner struct {
	scanner      *bufio.Scanner
	pending      []string
	current      string
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
		s.pending[0] = ""
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
		frame := parseSSEEvent(s.frame.lines)
		data := strings.TrimSpace(frame.data)
		if frame.hasData && data != "" && data != "[DONE]" && !json.Valid([]byte(data)) {
			// Keep the existing bounded repair for concatenated Responses JSON,
			// after assembling the complete SSE event. Other corruption is fatal.
			documents, repaired := splitOpenAIConcatenatedJSONDocuments([]byte(data))
			if !repaired {
				s.err = errors.New("invalid upstream SSE event JSON")
				return false
			}
			for i, document := range documents {
				value := string(document)
				if i == 0 {
					s.pending = append(s.pending, strings.Split(strings.TrimSuffix(frame.block("", &value), "\n"), "\n")...)
				} else {
					var envelope struct {
						Type string `json:"type"`
					}
					_ = json.Unmarshal(document, &envelope)
					s.pending = append(s.pending, "event: "+strings.TrimSpace(envelope.Type), "data: "+value, "")
				}
			}
		} else if data != "" && data != "[DONE]" && strings.Contains(frame.data, "\n") {
			// Canonicalize multi-line JSON to one data field for the existing
			// protocol consumers. Preserve id/retry/comments and unknown fields.
			var compact bytes.Buffer
			if err := json.Compact(&compact, []byte(frame.data)); err != nil {
				s.err = err
				return false
			}
			value := compact.String()
			s.pending = strings.Split(strings.TrimSuffix(frame.block("", &value), "\n"), "\n")
		} else {
			s.pending = append(s.pending, frame.lines...)
			s.pending = append(s.pending, "")
		}
		s.frame.reset()
		return s.Scan()
	}
	if s.scanner != nil {
		s.err = s.scanner.Err()
	}
	if s.err == nil && len(s.frame.lines) > 0 {
		s.err = io.ErrUnexpectedEOF
	}
	return false
}

func (s *openAISSEJSONDocumentScanner) Text() string {
	return s.current
}

func (s *openAISSEJSONDocumentScanner) Err() error {
	return s.err
}
