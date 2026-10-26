package basispoints

import (
	"strings"

	"github.com/tidwall/gjson"
)

// NativeClientToolChoiceEligible recognizes a self-contained client-tool choice
// that this bridge cannot carry. It neither rewrites the request nor promises
// provider support. Hosted omission and dynamic catalogs remain BPS decisions.
func NativeClientToolChoiceEligible(source map[string]any) bool {
	tools, ok := source["tools"].([]any)
	if !ok || len(tools) == 0 {
		return false
	}
	b := &Bridge{tools: make(map[string]tool)}
	if _, err := b.collectTools(tools, "", "tools", false); err != nil || len(b.tools) == 0 || len(b.unsupportedTools) != 0 {
		return false
	}
	for _, declared := range b.tools {
		if strings.TrimSpace(declared.Name) != declared.Name {
			return false
		}
		for _, namespace := range strings.Split(declared.Namespace, ".") {
			if strings.TrimSpace(namespace) != namespace {
				return false
			}
		}
	}
	switch input := source["input"].(type) {
	case string:
	case []any:
		for _, raw := range input {
			item, ok := raw.(object)
			if !ok {
				return false
			}
			switch strings.TrimSpace(text(item["type"])) {
			case "additional_tools", "tool_search_call", "tool_search_output", "web_search_call":
				// Do not migrate BPS discoveries or enable the native transform's
				// automatic hosted web_search declaration for replayed history.
				return false
			}
		}
	default:
		return false
	}
	matchesClientTool := func(raw any) bool {
		choice, ok := raw.(object)
		if !ok || len(choice) != 2 {
			return false
		}
		kind, name := text(choice["type"]), text(choice["name"])
		if (kind != "function" && kind != "custom") || name == "" || strings.TrimSpace(name) != name {
			return false
		}
		declared, exists := b.tools[name]
		return exists && declared.Kind == kind
	}
	if choice, ok := source["tool_choice"].(string); ok {
		return choice == "required"
	}
	choice, ok := source["tool_choice"].(object)
	if !ok || text(choice["type"]) != "allowed_tools" {
		return matchesClientTool(source["tool_choice"])
	}
	if len(choice) != 3 || (text(choice["mode"]) != "auto" && text(choice["mode"]) != "required") {
		return false
	}
	allowed, ok := choice["tools"].([]any)
	if !ok || len(allowed) == 0 {
		return false
	}
	for _, reference := range allowed {
		if !matchesClientTool(reference) {
			return false
		}
	}
	return true
}

// NativeFallbackReason reports declarations requiring the native Codex channel
// under the default fallback policy. This describes the bridge's capabilities,
// not whether the BPS upstream could support them through another protocol.
func NativeFallbackReason(body []byte) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	choice := gjson.GetBytes(body, "tool_choice")
	if choice.Type == gjson.String && choice.String() == "none" {
		return ""
	}
	switch choice.Get("type").String() {
	case "web_search", "web_search_preview", "web_search_preview_2025_03_11", "web_search_2025_08_26", "image_generation":
		return "tool_choice"
	}
	if tools := gjson.GetBytes(body, "tools"); tools.IsArray() {
		fallback := ""
		tools.ForEach(func(_, tool gjson.Result) bool {
			kind := strings.ToLower(strings.TrimSpace(tool.Get("type").String()))
			switch kind {
			case "image_generation":
				fallback = "image_generation"
				return false
			case "web_search", "web_search_preview", "web_search_preview_2025_03_11", "web_search_2025_08_26":
				if tool.Get("external_web_access").Bool() || tool.Get("search_context_size").String() == "high" {
					fallback = "web_search"
					return false
				}
			}
			return true
		})
		if fallback != "" {
			return fallback
		}
	}
	// Inline data images are handled by Sub2API's local relay before Prepare;
	// leave them on BPS so the relay can rewrite them to signed HTTPS URLs.
	return ""
}
