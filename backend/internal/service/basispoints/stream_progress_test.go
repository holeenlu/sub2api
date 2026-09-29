package basispoints

import (
	"context"
	"io"
	"strings"
	"testing"
)

func indexedMessageWire(finalText string) string {
	added := object{"type": "message", "id": "msg_1", "status": "in_progress", "content": []any{}}
	part := object{"type": "output_text", "text": ""}
	finalPart := object{"type": "output_text", "text": finalText}
	finalItem := object{"type": "message", "id": "msg_1", "status": "completed", "content": []any{finalPart}}
	return sse(object{"type": "response.created", "response": object{"id": "resp_1", "status": "in_progress", "output": []any{}}}) +
		sse(object{"type": "response.output_item.added", "output_index": 0, "item": added}) +
		sse(object{"type": "response.content_part.added", "output_index": 0, "content_index": 0, "item_id": "msg_1", "part": part}) +
		sse(object{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": "msg_1", "delta": "hello"}) +
		sse(object{"type": "response.output_text.done", "output_index": 0, "content_index": 0, "item_id": "msg_1", "text": "hello"}) +
		sse(object{"type": "response.content_part.done", "output_index": 0, "content_index": 0, "item_id": "msg_1", "part": object{"type": "output_text", "text": "hello"}}) +
		sse(object{"type": "response.output_item.done", "output_index": 0, "item": finalItem}) +
		sse(object{"type": "response.completed", "response": object{"id": "resp_1", "status": "completed", "output": []any{finalItem}}})
}

func TestStreamProgressRejectsTerminalTextMismatch(t *testing.T) {
	_, bridge := mustPrepare(t, object{"model": "gpt-5.6-sol", "stream": true, "input": "hello"}, "progress", nil)
	body := bridge.StreamWithRepairs(context.Background(), io.NopCloser(strings.NewReader(indexedMessageWire("goodbye"))), nil, nil)
	events := repairEvents(t, body)
	last := events[len(events)-1]
	if last["type"] != "response.failed" {
		t.Fatalf("mismatched terminal content must fail, got %v", last["type"])
	}
	response := repairValue[object](t, last["response"])
	if repairValue[object](t, response["error"])["code"] != "basispoints_protocol_error" {
		t.Fatalf("unexpected failure: %+v", response["error"])
	}
	for _, event := range events {
		if event["type"] == "response.completed" {
			t.Fatal("mismatched terminal content was reported as completed")
		}
	}
}

func TestStreamProgressAcceptsMatchingTerminalText(t *testing.T) {
	_, bridge := mustPrepare(t, object{"model": "gpt-5.6-sol", "stream": true, "input": "hello"}, "progress", nil)
	body := bridge.StreamWithRepairs(context.Background(), io.NopCloser(strings.NewReader(indexedMessageWire("hello"))), nil, nil)
	events := repairEvents(t, body)
	if events[len(events)-1]["type"] != "response.completed" {
		t.Fatalf("matching terminal content must complete, got %v", events[len(events)-1]["type"])
	}
}
