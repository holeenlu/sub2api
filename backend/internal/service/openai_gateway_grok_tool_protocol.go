package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const grokResponsesClientToolMappingContextKey = "grok_responses_client_tool_mapping"

func adaptResponsesClientToolsForFunctionUpstream(body []byte, upstream string) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	return adaptResponsesClientToolsForFunctionUpstreamWithMapping(
		body,
		upstream,
		apicompat.ResponsesClientToolMapping{},
	)
}

func adaptResponsesClientToolsForFunctionUpstreamWithMapping(
	body []byte,
	upstream string,
	inherited apicompat.ResponsesClientToolMapping,
	inheritedLoweredTools ...[]any,
) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var requestBody map[string]any
	if err := decoder.Decode(&requestBody); err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, fmt.Errorf("decode %s Responses client tools: %w", upstream, err)
	}

	mapping, changed, err := apicompat.AdaptResponsesClientToolsWithInheritedMapping(requestBody, inherited, inheritedLoweredTools...)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	if !changed {
		return body, mapping, nil
	}
	rebuilt, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, fmt.Errorf("encode %s Responses client tools: %w", upstream, err)
	}
	return rebuilt, mapping, nil
}

func adaptGrokResponsesClientTools(body []byte) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	return adaptResponsesClientToolsForFunctionUpstream(body, "Grok")
}

func hasResponsesClientToolMapping(mapping apicompat.ResponsesClientToolMapping) bool {
	return len(mapping.CustomTools) > 0 || mapping.ToolSearch || len(mapping.NamespaceTools) > 0
}

func hasGrokResponsesClientToolMapping(mapping apicompat.ResponsesClientToolMapping) bool {
	return hasResponsesClientToolMapping(mapping)
}

func setGrokResponsesClientToolMapping(c *gin.Context, mapping apicompat.ResponsesClientToolMapping) {
	if c == nil {
		return
	}
	if !hasGrokResponsesClientToolMapping(mapping) {
		clearGrokResponsesClientToolMapping(c)
		return
	}
	c.Set(grokResponsesClientToolMappingContextKey, mapping)
}

func clearGrokResponsesClientToolMapping(c *gin.Context) {
	if c == nil {
		return
	}
	if _, exists := c.Get(grokResponsesClientToolMappingContextKey); !exists {
		return
	}
	c.Set(grokResponsesClientToolMappingContextKey, apicompat.ResponsesClientToolMapping{})
}

func grokResponsesClientToolMapping(c *gin.Context) (apicompat.ResponsesClientToolMapping, bool) {
	if c == nil {
		return apicompat.ResponsesClientToolMapping{}, false
	}
	value, ok := c.Get(grokResponsesClientToolMappingContextKey)
	if !ok {
		return apicompat.ResponsesClientToolMapping{}, false
	}
	mapping, ok := value.(apicompat.ResponsesClientToolMapping)
	return mapping, ok && hasGrokResponsesClientToolMapping(mapping)
}

func restoreGrokResponsesClientToolPayload(c *gin.Context, payload []byte) ([]byte, error) {
	mapping, ok := grokResponsesClientToolMapping(c)
	if !ok || !bytes.Contains(payload, []byte(`"function_call"`)) || !json.Valid(payload) {
		return payload, nil
	}
	restored, _, err := apicompat.RestoreResponsesClientToolPayload(payload, mapping)
	return restored, err
}

type responsesClientToolStreamBody struct {
	*io.PipeReader
	source io.Closer
}

func (b *responsesClientToolStreamBody) Close() error {
	readerErr := b.PipeReader.Close()
	sourceErr := b.source.Close()
	if readerErr != nil {
		return readerErr
	}
	return sourceErr
}

func newResponsesClientToolStreamBody(
	source io.ReadCloser,
	mapping apicompat.ResponsesClientToolMapping,
	maxLineSize int,
) io.ReadCloser {
	reader, writer := io.Pipe()
	body := &responsesClientToolStreamBody{PipeReader: reader, source: source}
	go transformResponsesClientToolStream(source, writer, mapping, maxLineSize)
	return body
}

func newGrokResponsesClientToolStreamBody(
	source io.ReadCloser,
	mapping apicompat.ResponsesClientToolMapping,
	maxLineSize int,
) io.ReadCloser {
	return newResponsesClientToolStreamBody(source, mapping, maxLineSize)
}

func transformResponsesClientToolStream(
	source io.ReadCloser,
	destination *io.PipeWriter,
	mapping apicompat.ResponsesClientToolMapping,
	maxLineSize int,
) {
	defer func() { _ = source.Close() }()
	if maxLineSize <= 0 {
		maxLineSize = defaultMaxLineSize
	}

	scanner := bufio.NewScanner(source)
	scanner.Split(newSSEScanLines())
	scanBuf := getSSEScannerBuf64K()
	defer putSSEScannerBuf64K(scanBuf)
	scanner.Buffer(scanBuf[:0], maxLineSize)
	documents := newOpenAISSEJSONDocumentScanner(scanner, maxLineSize)
	restorer := apicompat.NewResponsesClientToolStreamRestorer(mapping)
	buffered := bufio.NewWriterSize(destination, 4*1024)
	for documents.Scan() {
		frame := documents.Frame()
		payloads := [][]byte{[]byte(frame.data)}
		if frame.hasData && strings.TrimSpace(frame.data) != "[DONE]" && strings.TrimSpace(frame.data) != "" {
			var err error
			payloads, _, err = restorer.RestoreEvent(payloads[0])
			if err != nil {
				_ = buffered.Flush()
				_ = destination.CloseWithError(fmt.Errorf("restore Responses client tool event: %w", err))
				return
			}
		}
		for index, payload := range payloads {
			out := frame
			eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
			if index > 0 {
				out = newOpenAISSEFrame(sseEvent{data: string(payload), hasData: true, lines: []string{"data: " + string(payload)}})
				if frame.hasEvent && eventType != "" {
					out.lines = append([]string{"event: " + eventType}, out.lines...)
					out.hasEvent = true
				}
			} else if out.hasEvent && eventType != "" && eventType != out.eventName {
				out.lines = append([]string(nil), out.lines...)
				for i, line := range out.lines {
					if field, _, ok := parseSSELine(line); ok && field == "event" {
						out.lines[i] = "event: " + eventType
					}
				}
			}
			if _, err := buffered.WriteString(out.block(string(payload))); err != nil {
				_ = destination.CloseWithError(err)
				return
			}
		}
		if err := buffered.Flush(); err != nil {
			_ = destination.CloseWithError(err)
			return
		}
	}

	if err := buffered.Flush(); err != nil {
		_ = destination.CloseWithError(err)
		return
	}
	if err := documents.Err(); err != nil {
		_ = destination.CloseWithError(err)
		return
	}
	_ = destination.Close()
}
