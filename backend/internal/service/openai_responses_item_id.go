package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func openAIResponsesInputItemIDPrefix(itemType string) (string, bool) {
	switch strings.TrimSpace(itemType) {
	case "message":
		return "msg", true
	case "reasoning":
		return "rs", true
	case "web_search_call":
		return "ws", true
	case "custom_tool_call":
		return openAIResponsesToolCallIDPrefix(itemType), true
	case "tool_search_call":
		return openAIResponsesToolCallIDPrefix(itemType), true
	case "custom_tool_call_output":
		// Although custom calls use ctc IDs, OpenAI validates replayed custom
		// call output item IDs against the generic fc namespace.
		return "fc", true
	default:
		if isCodexToolCallInputType(itemType) {
			return openAIResponsesToolCallIDPrefix(itemType), true
		}
		return "", false
	}
}

func openAIResponsesToolCallIDPrefix(itemType string) string {
	switch strings.TrimSpace(itemType) {
	case "custom_tool_call", "custom_tool_call_output":
		return "ctc"
	case "tool_search_call", "tool_search_output":
		return "tsc"
	default:
		return "fc"
	}
}

// Invalid replayed IDs are removed rather than rewritten because a fabricated
// ID may point at a different upstream object.
func shouldStripOpenAIResponsesInputItemID(itemType, id string) bool {
	prefix, constrained := openAIResponsesInputItemIDPrefix(itemType)
	if !constrained {
		return false
	}
	return id == "" || len(id) > 64 || !strings.HasPrefix(id, prefix)
}

func shouldStripOpenAIResponsesNonPairCallID(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "message", "reasoning", "image_generation_call":
		return true
	default:
		return false
	}
}

func sanitizeOpenAIResponsesInputItemIDs(body []byte) ([]byte, bool, error) {
	input := parseRawJSONView(body).Get("input")
	if !input.IsArray() {
		return body, false, nil
	}

	type inputItem struct {
		raw         string
		stripID     bool
		stripCallID bool
	}

	items := make([]inputItem, 0)
	input.ForEach(func(_, item gjson.Result) bool {
		parsed := inputItem{raw: item.Raw}
		if item.IsObject() {
			itemType := item.Get("type")
			id := item.Get("id")
			trimmedItemType := strings.TrimSpace(itemType.String())
			parsed.stripCallID = item.Get("call_id").Exists() && shouldStripOpenAIResponsesNonPairCallID(trimmedItemType)
			if id.Type == gjson.String {
				parsed.stripID = shouldStripOpenAIResponsesInputItemID(trimmedItemType, id.String())
			}
		}
		items = append(items, parsed)
		return true
	})
	hasSanitization := false
	for _, item := range items {
		if item.stripID || item.stripCallID {
			hasSanitization = true
			break
		}
	}
	if !hasSanitization {
		return body, false, nil
	}

	rebuiltItems := make([]string, 0, len(items))
	for index, item := range items {
		if !item.stripID && !item.stripCallID {
			rebuiltItems = append(rebuiltItems, item.raw)
			continue
		}
		itemBody := []byte(item.raw)
		if item.stripID {
			var err error
			itemBody, err = sjson.DeleteBytes(itemBody, "id")
			if err != nil {
				return nil, false, fmt.Errorf("delete input.%d.id: %w", index, err)
			}
		}
		if item.stripCallID {
			var err error
			itemBody, err = sjson.DeleteBytes(itemBody, "call_id")
			if err != nil {
				return nil, false, fmt.Errorf("delete input.%d.call_id: %w", index, err)
			}
		}
		rebuiltItems = append(rebuiltItems, string(itemBody))
	}
	return replaceOpenAIRawInput(body, input, rebuiltItems), true, nil
}

// Only an explicit upstream lookup rejection permits removing valid IDs. A
// reference or an incomplete item still needs persisted state: never turn it
// into an empty item or silently drop it. Tool pairing uses call_id, not id.
//
// The third result explains why a repair was declined, and the caller logs it.
// Without that line a declined repair looks exactly like "the upstream never
// sent this rejection", which is how the rejection stayed unhandled unnoticed.
// "unsupported item type" specifically means the switch below could grow to
// cover it; "incomplete" means the payload genuinely cannot be replayed inline.
func removeOpenAIResponsesSelfContainedItemIDs(body []byte) ([]byte, bool, string, error) {
	input := parseRawJSONView(body).Get("input")
	if !input.IsArray() {
		return body, false, "input is not an array", nil
	}
	items := input.Array()
	for index, item := range items {
		typ := strings.TrimSpace(item.Get("type").String())
		if typ == "item_reference" {
			return body, false, fmt.Sprintf("item_reference at input[%d]", index), nil
		}
		if !item.Get("id").Exists() {
			continue
		}
		known := true
		selfContained := false
		switch typ {
		case "", "message":
			selfContained = item.Get("role").String() != "" &&
				(item.Get("content").Type == gjson.String || item.Get("content").IsArray())
		case "reasoning":
			selfContained = strings.TrimSpace(item.Get("encrypted_content").String()) != ""
		case "function_call":
			selfContained = item.Get("call_id").String() != "" && item.Get("name").String() != "" && item.Get("arguments").Type == gjson.String
		case "custom_tool_call":
			selfContained = item.Get("call_id").String() != "" && item.Get("name").String() != "" && item.Get("input").Type == gjson.String
		case "function_call_output", "custom_tool_call_output":
			selfContained = item.Get("call_id").String() != "" &&
				(item.Get("output").Type == gjson.String || item.Get("output").IsArray())
		default:
			known = false
		}
		if !selfContained {
			if !known {
				return body, false, fmt.Sprintf("unsupported item type %q at input[%d]", typ, index), nil
			}
			label := typ
			if label == "" {
				label = "message"
			}
			return body, false, fmt.Sprintf("incomplete %s at input[%d]", label, index), nil
		}
	}
	changed := false
	rebuilt := make([]string, 0, len(items))
	for _, item := range items {
		raw := item.Raw
		if item.Get("id").Exists() {
			var err error
			raw, err = sjson.Delete(raw, "id")
			if err != nil {
				return nil, false, "", fmt.Errorf("delete rejected input item id: %w", err)
			}
			changed = true
		}
		rebuilt = append(rebuilt, raw)
	}
	if !changed {
		return body, false, "no input item ids to remove", nil
	}
	return replaceOpenAIRawInput(body, input, rebuilt), true, "", nil
}
