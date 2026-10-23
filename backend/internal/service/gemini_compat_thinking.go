package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Thought visibility is separate from effort and manual budgets, which are
// normalized once by applyClaudeThinkingToGemini after account model mapping.
func includeGeminiCompatThoughts(body []byte, model string, enabled bool) []byte {
	_, _, _, _, known := geminiThinkingCapabilities(model)
	if !known || !enabled || gjson.GetBytes(body, "generationConfig.thinkingConfig.includeThoughts").Exists() {
		return body
	}
	budget := gjson.GetBytes(body, "generationConfig.thinkingConfig.thinkingBudget")
	if budget.Exists() && budget.Int() == 0 {
		return body
	}
	out, err := sjson.SetBytes(body, "generationConfig.thinkingConfig.includeThoughts", true)
	if err != nil {
		return body
	}
	return out
}

func claudeRequestEnablesThinking(body []byte) bool {
	switch gjson.GetBytes(body, "thinking.type").String() {
	case "enabled", "adaptive":
		return true
	default:
		return false
	}
}

// isGeminiThoughtPart 判断响应 part 是否为思考摘要。
func isGeminiThoughtPart(part map[string]any) bool {
	thought, _ := part["thought"].(bool)
	return thought
}

// dropClaudeHistoryThinkingBlocks 删除 Anthropic 请求历史消息里的 thinking/redacted_thinking 块，
// 只含思考块的消息整条删除；与 convertClaudeMessagesToGeminiContents 的取舍一致，供签名降级
// 重试在把思考块改写成正文之前使用。
func dropClaudeHistoryThinkingBlocks(claudeBody []byte) []byte {
	var req map[string]any
	if err := json.Unmarshal(claudeBody, &req); err != nil {
		return claudeBody
	}
	messages, ok := req["messages"].([]any)
	if !ok {
		return claudeBody
	}
	changed := false
	kept := make([]any, 0, len(messages))
	for _, m := range messages {
		mm, ok := m.(map[string]any)
		if !ok {
			kept = append(kept, m)
			continue
		}
		blocks, ok := mm["content"].([]any)
		if !ok {
			kept = append(kept, m)
			continue
		}
		filtered := make([]any, 0, len(blocks))
		for _, block := range blocks {
			if bm, ok := block.(map[string]any); ok {
				if bt, _ := bm["type"].(string); bt == "thinking" || bt == "redacted_thinking" {
					continue
				}
			}
			filtered = append(filtered, block)
		}
		if len(filtered) == len(blocks) {
			kept = append(kept, m)
			continue
		}
		// 原消息是多块，转换器会过滤空白正文块；删掉思考块后同样过滤，避免剩下的单块空白正文被原样发出。
		nonBlank := filtered[:0]
		for _, block := range filtered {
			if bm, ok := block.(map[string]any); ok {
				if bt, _ := bm["type"].(string); bt == "text" {
					if text, _ := bm["text"].(string); strings.TrimSpace(text) == "" {
						continue
					}
				}
			}
			nonBlank = append(nonBlank, block)
		}
		filtered = nonBlank
		changed = true
		if len(filtered) == 0 {
			continue
		}
		mm["content"] = filtered
		kept = append(kept, mm)
	}
	if !changed {
		return claudeBody
	}
	req["messages"] = kept
	out, err := json.Marshal(req)
	if err != nil {
		return claudeBody
	}
	return out
}

// dropChatReasoningContentParts 删除 Chat Completions assistant 消息 content 数组里
// type 为 thinking/reasoning 的部分；字符串形态的 content 原样返回。
func dropChatReasoningContentParts(content json.RawMessage) json.RawMessage {
	if !bytes.HasPrefix(bytes.TrimSpace(content), []byte("[")) {
		return content
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(content, &parts); err != nil {
		return content
	}
	kept := make([]json.RawMessage, 0, len(parts))
	for _, part := range parts {
		switch gjson.GetBytes(part, "type").String() {
		case "thinking", "reasoning":
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == len(parts) {
		return content
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return content
	}
	return out
}
