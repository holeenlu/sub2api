package service

import (
	"bufio"
	"strings"

	"github.com/tidwall/gjson"
)

type openAISSEDataAccumulator struct {
	lines []string
}

func (a *openAISSEDataAccumulator) AddLine(line string, fn func([]byte)) {
	if fn == nil {
		return
	}
	trimmedLine := strings.TrimRight(line, "\r\n")
	if data, ok := extractOpenAISSEDataLine(trimmedLine); ok {
		a.lines = append(a.lines, data)
		return
	}
	if trimmedLine == "" {
		a.Flush(fn)
	}
}

func (a *openAISSEDataAccumulator) Flush(fn func([]byte)) {
	if fn == nil || len(a.lines) == 0 {
		return
	}
	emitOpenAISSEDataPayloads(a.lines, fn)
	clear(a.lines)
	a.lines = a.lines[:0]
}

func forEachOpenAISSEDataPayload(body string, fn func([]byte)) {
	if fn == nil {
		return
	}
	forEachOpenAISSEFrame(body, func(_ string, data []byte) { fn(data) })
}

func forEachOpenAISSEFrame(body string, fn func(string, []byte)) {
	if fn == nil || body == "" {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Split(newSSEScanLines())
	scanner.Buffer(make([]byte, 64*1024), defaultMaxLineSize)
	frames := newOpenAISSEJSONDocumentScanner(scanner)
	for frames.Scan() {
		frame := frames.Frame()
		if !frame.hasData || frame.data == "" || strings.TrimSpace(frame.data) == "[DONE]" {
			continue
		}
		fn(frame.eventType, []byte(frame.data))
	}
	// Pending EOF frames and malformed JSON never become terminal/usage events.
}

func emitOpenAISSEDataPayloads(lines []string, fn func([]byte)) {
	if fn == nil || len(lines) == 0 {
		return
	}
	joined := strings.Join(lines, "\n")
	if gjson.Valid(joined) {
		emitOpenAISSEDataPayload(joined, fn)
	}
}

func emitOpenAISSEDataPayload(data string, fn func([]byte)) {
	data = strings.TrimSpace(data)
	if data == "" || data == "[DONE]" {
		return
	}
	fn([]byte(data))
}
