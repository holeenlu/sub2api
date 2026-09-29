package basispoints

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// streamProgress records only fully indexed plaintext message/reasoning
// events. Older gateways sometimes omit indexes or lifecycle fields; those
// events remain pass-through for compatibility. When the gateway does provide
// the complete identity, the terminal response is checked before any tool
// translation or replay-cache write can occur.
type streamProgress struct {
	responseID string
	messages   map[int]*streamProgressItem
	reasoning  map[int]*streamProgressItem
}

type streamProgressItem struct {
	id    string
	parts map[int]*streamProgressPart
}

type streamProgressPart struct {
	kind     string
	text     strings.Builder
	textDone bool
	done     bool
}

func newStreamProgress() *streamProgress {
	return &streamProgress{messages: make(map[int]*streamProgressItem), reasoning: make(map[int]*streamProgressItem)}
}

func progressIndex(value any) (int, bool) {
	switch value := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseInt(value.String(), 10, 31)
		return int(parsed), err == nil && parsed >= 0
	case int:
		return value, value >= 0
	case int64:
		return int(value), value >= 0 && value <= 1<<30
	case float64:
		return int(value), value >= 0 && value == float64(int(value)) && value <= 1<<30
	default:
		return 0, false
	}
}

func progressItem(value any) object {
	item, _ := value.(object)
	return item
}

func progressPart(value any) object {
	part, _ := value.(object)
	return part
}

func (p *streamProgress) observe(kind string, payload object) error {
	switch kind {
	case "response.created", "response.in_progress":
		response := progressItem(payload["response"])
		if id := text(response["id"]); id != "" {
			if p.responseID != "" && p.responseID != id {
				return p.mismatch()
			}
			p.responseID = id
		}
	case "response.output_item.added":
		index, ok := progressIndex(payload["output_index"])
		item := progressItem(payload["item"])
		id, itemType := text(item["id"]), text(item["type"])
		if !ok || id == "" || (itemType != "message" && itemType != "reasoning") {
			return nil
		}
		items := p.messages
		if itemType == "reasoning" {
			items = p.reasoning
		}
		if previous := items[index]; previous != nil && previous.id != id {
			return p.mismatch()
		}
		if items[index] == nil {
			items[index] = &streamProgressItem{id: id, parts: make(map[int]*streamProgressPart)}
		}
	case "response.content_part.added":
		index, indexOK := progressIndex(payload["output_index"])
		partIndex, partOK := progressIndex(payload["content_index"])
		itemID := text(payload["item_id"])
		part := progressPart(payload["part"])
		if !indexOK || !partOK || itemID == "" || text(part["type"]) == "" {
			return nil
		}
		item := p.messages[index]
		if item == nil || item.id != itemID {
			return nil
		}
		if previous := item.parts[partIndex]; previous != nil && previous.kind != text(part["type"]) {
			return p.mismatch()
		}
		if item.parts[partIndex] == nil {
			item.parts[partIndex] = &streamProgressPart{kind: text(part["type"])}
		}
	case "response.reasoning_summary_part.added":
		index, indexOK := progressIndex(payload["output_index"])
		partIndex, partOK := progressIndex(payload["summary_index"])
		itemID := text(payload["item_id"])
		part := progressPart(payload["part"])
		if !indexOK || !partOK || itemID == "" || text(part["type"]) == "" {
			return nil
		}
		item := p.reasoning[index]
		if item == nil || item.id != itemID {
			return nil
		}
		if previous := item.parts[partIndex]; previous != nil && previous.kind != text(part["type"]) {
			return p.mismatch()
		}
		if item.parts[partIndex] == nil {
			item.parts[partIndex] = &streamProgressPart{kind: text(part["type"])}
		}
	case "response.output_text.delta":
		index, indexOK := progressIndex(payload["output_index"])
		partIndex, partOK := progressIndex(payload["content_index"])
		itemID, delta := text(payload["item_id"]), text(payload["delta"])
		if !indexOK || !partOK || itemID == "" || delta == "" {
			return nil
		}
		item := p.messages[index]
		part := itemPart(item, itemID, partIndex)
		if part == nil || part.kind != "output_text" || part.textDone || part.done {
			return nil
		}
		part.text.WriteString(delta)
	case "response.output_text.done":
		index, indexOK := progressIndex(payload["output_index"])
		partIndex, partOK := progressIndex(payload["content_index"])
		itemID, doneText := text(payload["item_id"]), text(payload["text"])
		if !indexOK || !partOK || itemID == "" {
			return nil
		}
		part := itemPart(p.messages[index], itemID, partIndex)
		if part == nil || part.kind != "output_text" || part.done {
			return nil
		}
		if doneText != part.text.String() {
			return p.mismatch()
		}
		part.textDone = true
	case "response.content_part.done":
		index, indexOK := progressIndex(payload["output_index"])
		partIndex, partOK := progressIndex(payload["content_index"])
		itemID := text(payload["item_id"])
		if !indexOK || !partOK || itemID == "" {
			return nil
		}
		part := itemPart(p.messages[index], itemID, partIndex)
		completed := progressPart(payload["part"])
		if part == nil || completed == nil || text(completed["type"]) != part.kind {
			return nil
		}
		if part.kind == "output_text" && text(completed["text"]) != part.text.String() {
			return p.mismatch()
		}
		part.done = true
	case "response.reasoning_summary_text.delta":
		index, indexOK := progressIndex(payload["output_index"])
		partIndex, partOK := progressIndex(payload["summary_index"])
		itemID, delta := text(payload["item_id"]), text(payload["delta"])
		if !indexOK || !partOK || itemID == "" || delta == "" {
			return nil
		}
		part := itemPart(p.reasoning[index], itemID, partIndex)
		if part == nil || part.kind != "summary_text" || part.textDone || part.done {
			return nil
		}
		part.text.WriteString(delta)
	case "response.reasoning_summary_text.done":
		index, indexOK := progressIndex(payload["output_index"])
		partIndex, partOK := progressIndex(payload["summary_index"])
		itemID, doneText := text(payload["item_id"]), text(payload["text"])
		if !indexOK || !partOK || itemID == "" {
			return nil
		}
		part := itemPart(p.reasoning[index], itemID, partIndex)
		if part == nil || part.kind != "summary_text" || part.done {
			return nil
		}
		if doneText != part.text.String() {
			return p.mismatch()
		}
		part.textDone = true
	case "response.reasoning_summary_part.done":
		index, indexOK := progressIndex(payload["output_index"])
		partIndex, partOK := progressIndex(payload["summary_index"])
		itemID := text(payload["item_id"])
		if !indexOK || !partOK || itemID == "" {
			return nil
		}
		part := itemPart(p.reasoning[index], itemID, partIndex)
		completed := progressPart(payload["part"])
		if part == nil || completed == nil || text(completed["type"]) != part.kind {
			return nil
		}
		if part.kind == "summary_text" && text(completed["text"]) != part.text.String() {
			return p.mismatch()
		}
		part.done = true
	}
	return nil
}

func itemPart(item *streamProgressItem, id string, index int) *streamProgressPart {
	if item == nil || item.id != id {
		return nil
	}
	return item.parts[index]
}

func (p *streamProgress) validate(response object) error {
	if len(p.messages) == 0 && len(p.reasoning) == 0 {
		return nil
	}
	if p.responseID != "" && text(response["id"]) != p.responseID {
		return p.mismatch()
	}
	output, _ := response["output"].([]any)
	for index, item := range p.messages {
		if !validateProgressItem(output, index, item, "message", "content") {
			return p.mismatch()
		}
	}
	for index, item := range p.reasoning {
		if !validateProgressItem(output, index, item, "reasoning", "summary") {
			return p.mismatch()
		}
	}
	return nil
}

func validateProgressItem(output []any, index int, tracked *streamProgressItem, wantType, partsField string) bool {
	if index < 0 || index >= len(output) {
		return false
	}
	item := progressItem(output[index])
	if text(item["type"]) != wantType || text(item["id"]) != tracked.id {
		return false
	}
	parts, _ := item[partsField].([]any)
	for partIndex, trackedPart := range tracked.parts {
		if partIndex < 0 || partIndex >= len(parts) {
			return false
		}
		part := progressPart(parts[partIndex])
		if text(part["type"]) != trackedPart.kind {
			return false
		}
		if trackedPart.kind == "output_text" || trackedPart.kind == "summary_text" {
			finalText := text(part["text"])
			if !strings.HasPrefix(finalText, trackedPart.text.String()) || (trackedPart.textDone && finalText != trackedPart.text.String()) {
				return false
			}
		}
	}
	return true
}

func (p *streamProgress) mismatch() error {
	return fmt.Errorf("basispoints stream final response does not match delivered output")
}
