package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unsafe"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	blockTypeServerToolUse       = "server_tool_use"
	blockTypeWebSearchToolResult = "web_search_tool_result"
)

// Fast-path byte patterns: both block types only ever appear as quoted JSON
// string values, so a raw substring check is a safe pre-filter regardless of
// key/value spacing.
var (
	patternServerToolUse       = []byte(`"server_tool_use"`)
	patternWebSearchToolResult = []byte(`"web_search_tool_result"`)
)

// FilterWebSearchHistoryBlocks removes web-search content blocks from
// historical messages when the upstream cannot accept them:
//
//  1. Emulation-synthesized blocks — server_tool_use / web_search_tool_result
//     whose tool-use ID carries webSearchToolUseIDPrefix — are fabricated
//     locally by the web-search emulation (gateway_websearch_emulation.go).
//     No upstream ever issued them, so clients replaying the conversation
//     (e.g. Claude Code) poison every follow-up request. They are stripped
//     for all upstreams.
//  2. For passback-required upstreams (DeepSeek/Kimi/GLM …, see
//     ResolveThinkingProtocol) all server_tool_use / web_search_tool_result
//     blocks are stripped: these upstreams only accept
//     text/thinking/image/tool_use/tool_result and reject anything else with
//     400 "invalid value: `server_tool_use`".
//
// Genuine search history without a native web_search declaration is converted
// to visible text; tools with only a client function name do not count as native.
// Declared native history stays intact. Adapted from dofastted/vm2api 4732e71d;
// its LICENSE/NOTICE and the confirmed commercial authorization still apply.
//
// The emulated assistant turn always carries a trailing text summary, so the
// search context survives the strip. A message whose content would become
// empty gets a placeholder text block (mirroring FilterThinkingBlocksForRetry).
// Returns the original body unchanged when nothing needs stripping.
func FilterWebSearchHistoryBlocks(body []byte, mappedModel string) []byte {
	if !bytes.Contains(body, patternServerToolUse) && !bytes.Contains(body, patternWebSearchToolResult) &&
		!bytes.Contains(body, []byte(`"search_result"`)) && !bytes.Contains(body, []byte(`"web_search_result"`)) {
		return body
	}

	stripAll := ResolveThinkingProtocol(mappedModel) == ThinkingProtocolPassbackRequired
	flattenSearch := true
	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		if strings.HasPrefix(tool.Get("type").String(), toolTypeWebSearchPrefix) {
			flattenSearch = false
			break
		}
	}

	jsonStr := *(*string)(unsafe.Pointer(&body))
	msgsRes := gjson.Get(jsonStr, "messages")
	if !msgsRes.Exists() || !msgsRes.IsArray() {
		return body
	}

	var messages []any
	decoder := json.NewDecoder(bytes.NewReader(sliceRawFromBody(body, msgsRes)))
	decoder.UseNumber() // Rewriting search history must not round other tool arguments.
	if err := decoder.Decode(&messages); err != nil {
		return body
	}

	modified := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msgMap["content"].([]any)
		if !ok {
			continue
		}

		// 延迟分配：只有命中需剥离的块才构建新 slice。
		var newContent []any
		for i, block := range content {
			blockMap, isMap := block.(map[string]any)
			if isMap && shouldStripWebSearchBlock(blockMap, stripAll) {
				if newContent == nil {
					newContent = make([]any, 0, len(content))
					newContent = append(newContent, content[:i]...)
				}
				continue
			}
			next := block
			blockChanged := false
			if isMap && flattenSearch {
				if text, ok := undeclaredSearchHistoryText(blockMap); ok {
					textBlock := map[string]any{"type": "text", "text": text}
					if control, ok := blockMap["cache_control"]; ok {
						textBlock["cache_control"] = control
					}
					next = textBlock
					blockChanged = true
				} else if blockMap["type"] == "tool_result" {
					// Client calls/results keep their identity; only encrypted search
					// artifacts inside a result become visible text.
					if nested, ok := blockMap["content"].([]any); ok {
						for j, inner := range nested {
							if item, ok := inner.(map[string]any); ok {
								if text, ok := undeclaredSearchHistoryText(item); ok {
									nested[j] = map[string]any{"type": "text", "text": text}
									modified = true
								}
							}
						}
					}
				}
			}
			if blockChanged && newContent == nil {
				newContent = append(make([]any, 0, len(content)), content[:i]...)
			}
			if newContent != nil {
				newContent = append(newContent, next)
			}
		}
		if newContent == nil {
			continue
		}
		modified = true
		if len(newContent) == 0 {
			role, _ := msgMap["role"].(string)
			placeholder := "(content removed)"
			if role == "assistant" {
				placeholder = "(assistant content removed)"
			}
			newContent = []any{map[string]any{"type": "text", "text": placeholder}}
		}
		msgMap["content"] = newContent
	}

	if !modified {
		return body
	}

	msgsBytes, err := json.Marshal(messages)
	if err != nil {
		return body
	}
	out, err := sjson.SetRawBytes(body, "messages", msgsBytes)
	if err != nil {
		return body
	}
	return out
}

func shouldStripWebSearchBlock(block map[string]any, stripAll bool) bool {
	blockType, _ := block["type"].(string)
	switch blockType {
	case blockTypeServerToolUse:
		if stripAll {
			return true
		}
		id, _ := block["id"].(string)
		return strings.HasPrefix(id, webSearchToolUseIDPrefix)
	case blockTypeWebSearchToolResult:
		if stripAll {
			return true
		}
		id, _ := block["tool_use_id"].(string)
		return strings.HasPrefix(id, webSearchToolUseIDPrefix)
	default:
		return false
	}
}

// Only search artifacts are converted: unrelated server tools and encrypted
// thinking remain under their existing protocol rules.
func undeclaredSearchHistoryText(block map[string]any) (string, bool) {
	kind, _ := block["type"].(string)
	switch kind {
	case blockTypeServerToolUse:
		name, _ := block["name"].(string)
		if name != toolNameWebSearch {
			return "", false
		}
		input, _ := block["input"].(map[string]any)
		query, _ := input["query"].(string)
		return "[web search] " + query, true
	case blockTypeWebSearchToolResult:
		if failure, ok := block["content"].(map[string]any); ok {
			code, _ := failure["error_code"].(string)
			return "[web search unavailable: " + code + "]", true
		}
		results, _ := block["content"].([]any)
		var text strings.Builder
		text.WriteString("[web search results]")
		for i, result := range results {
			if item, ok := result.(map[string]any); ok {
				fmt.Fprintf(&text, "\n%d. %s", i+1, visibleSearchResult(item))
			}
		}
		return text.String(), true
	case "search_result", "web_search_result":
		if _, encrypted := block["encrypted_content"].(string); encrypted {
			return "[search result] " + visibleSearchResult(block), true
		}
	}
	return "", false
}

func visibleSearchResult(result map[string]any) string {
	title, _ := result["title"].(string)
	url, _ := result["url"].(string)
	if url == "" {
		url, _ = result["source"].(string)
	}
	var parts []string
	for _, value := range []string{title, url} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	head := strings.Join(parts, " — ")
	if head == "" {
		head = "result"
	}
	var content []string
	switch value := result["content"].(type) {
	case string:
		content = append(content, value)
	case []any:
		for _, block := range value {
			if item, ok := block.(map[string]any); ok && item["type"] == "text" {
				if text, ok := item["text"].(string); ok {
					content = append(content, text)
				}
			}
		}
	}
	if len(content) > 0 {
		head += "\n" + strings.Join(content, "\n")
	}
	return head
}
