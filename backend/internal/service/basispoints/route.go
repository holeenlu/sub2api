package basispoints

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// isHostedToolChoice reports a forced selection of a hosted capability that
// the bridge never executes.
func isHostedToolChoice(kind string) bool {
	switch kind {
	case "web_search", "web_search_preview", "web_search_preview_2025_03_11", "web_search_2025_08_26", "image_generation":
		return true
	}
	return false
}

// RelaxHostedToolChoice turns a forced hosted-tool selection into automatic
// mode. The declaration itself is omitted by the bridge with a notice, so the
// model can still answer or use a declared client tool instead of the request
// failing. Forced client functions and "required" are left for Prepare.
func RelaxHostedToolChoice(body []byte) ([]byte, string, error) {
	if !gjson.ValidBytes(body) {
		return body, "", nil
	}
	kind := gjson.GetBytes(body, "tool_choice.type").String()
	if !isHostedToolChoice(kind) {
		return body, "", nil
	}
	relaxed, err := sjson.SetBytes(body, "tool_choice", "auto")
	if err != nil {
		return nil, "", err
	}
	return relaxed, kind, nil
}

// NativeFallbackReason identifies hosted capabilities requiring the native channel.
// Normal requests may fall back to native Codex according to account policy;
// BPS-only probes reject these capabilities before dispatch.
func NativeFallbackReason(body []byte) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	choice := gjson.GetBytes(body, "tool_choice")
	if choice.Type == gjson.String && choice.String() == "none" {
		return ""
	}
	if isHostedToolChoice(choice.Get("type").String()) {
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
